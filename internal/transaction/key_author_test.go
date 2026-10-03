package transaction_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

func newAuthorService(store transaction.Store, periodID uuid.UUID, uow transaction.UnitOfWork) *transaction.Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return transaction.NewService(store, log, apperror.NewReporter(log, false).Unexpected,
		transaction.WalletReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.WalletInfo, error) {
			return transaction.WalletInfo{ID: id, Currency: money.IDR}, nil
		}),
		transaction.CategoryReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.CategoryInfo, error) {
			return transaction.CategoryInfo{ID: id, Kind: "expense"}, nil
		}),
		&periodStub{
			infos:     map[uuid.UUID]transaction.PeriodInfo{periodID: {ID: periodID}},
			byInstant: func(time.Time) (uuid.UUID, bool) { return periodID, true },
		},
		uow,
	)
}

func keyPrincipalCtx(ctx context.Context, tc tenant.Context, keyID uuid.UUID) context.Context {
	ctx = session.PrincipalInto(ctx, session.Principal{KeyID: keyID, Source: session.SourceAPIKey})
	return tenant.Into(ctx, tenant.Context{OrgID: tc.OrgID, ProjectID: tc.ProjectID})
}

func personCtx(ctx context.Context, tc tenant.Context) context.Context {
	ctx = session.PrincipalInto(ctx, session.Principal{UserID: tc.UserID, Source: session.SourceLocal})
	return tenant.Into(ctx, tc)
}

type authorRow struct {
	createdBy sql.NullString
	keyID     sql.NullString
}

func readTxnAuthor(t *testing.T, sqlDB *sql.DB, query string, id uuid.UUID) authorRow {
	t.Helper()
	var r authorRow
	require.NoError(t, sqlDB.QueryRowContext(t.Context(), query, id.String()).Scan(&r.createdBy, &r.keyID))
	return r
}

func assertKeyAuthored(t *testing.T, r authorRow, keyID uuid.UUID) {
	t.Helper()
	assert.False(t, r.createdBy.Valid, "a key-recorded row must have created_by NULL, got %q", r.createdBy.String)
	require.True(t, r.keyID.Valid, "a key-recorded row must name its key")
	assert.Equal(t, keyID.String(), r.keyID.String)
}

func assertPersonAuthored(t *testing.T, r authorRow, userID uuid.UUID) {
	t.Helper()
	require.True(t, r.createdBy.Valid, "a person-recorded row must name its user")
	assert.Equal(t, userID.String(), r.createdBy.String)
	assert.False(t, r.keyID.Valid, "a person-recorded row must have created_by_key_id NULL, got %q", r.keyID.String)
}

func TestSQLite_Transaction_KeyAuthor(t *testing.T) {
	f := newSQLiteFixture(t)
	keyID := uuid.New()
	mustExec(t, f.db,
		"INSERT INTO "+f.prefix+"api_keys (id, org_id, project_id, name, secret_hash, created_at) VALUES (?, ?, ?, 'ci', x'01', ?)",
		keyID.String(), f.tc.OrgID.String(), f.tc.ProjectID.String(), "2026-08-01T00:00:00Z")
	svc := newAuthorService(f.store, f.period,
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	query := "SELECT created_by, created_by_key_id FROM " + f.prefix + "transactions WHERE id = ?"
	at := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	byKey, err := svc.Record(keyPrincipalCtx(t.Context(), f.tc, keyID), transaction.RecordInput{
		WalletID: f.walletA, Kind: transaction.KindIncome, Amount: money.New(1_000, money.IDR), OccurredAt: at,
	})
	require.NoError(t, err)
	assert.Equal(t, keyID, byKey.CreatedByKeyID)
	assert.Equal(t, uuid.Nil, byKey.CreatedBy)
	assertKeyAuthored(t, readTxnAuthor(t, f.db, query, byKey.ID), keyID)

	got, err := f.store.ByID(f.ctx(), byKey.ID)
	require.NoError(t, err)
	assert.Equal(t, keyID, got.CreatedByKeyID, "the store must read the key author back")
	assert.Equal(t, uuid.Nil, got.CreatedBy)

	require.NoError(t, svc.RecordOpening(keyPrincipalCtx(t.Context(), f.tc, keyID), f.walletB, money.New(5_000, money.IDR), at, uuid.Nil))

	byPerson, err := svc.Record(personCtx(t.Context(), f.tc), transaction.RecordInput{
		WalletID: f.walletA, Kind: transaction.KindIncome, Amount: money.New(2_000, money.IDR), OccurredAt: at,
	})
	require.NoError(t, err)
	assertPersonAuthored(t, readTxnAuthor(t, f.db, query, byPerson.ID), f.tc.UserID)
}

const nilAuthor = "00000000-0000-0000-0000-000000000000"

func checkLegacyNilAuthorRevises(t *testing.T, sqlDB *sql.DB, svc *transaction.Service, tc tenant.Context, id uuid.UUID, query string) {
	t.Helper()
	got, err := svc.Revise(personCtx(t.Context(), tc), id, transaction.RevisePatch{Note: ptr("revised")})
	require.NoError(t, err, "a legacy row authored by the nil uuid must stay revisable")
	assert.Equal(t, "revised", got.Note)
	r := readTxnAuthor(t, sqlDB, query, id)
	require.True(t, r.createdBy.Valid, "the legacy author must not become NULL")
	assert.Equal(t, nilAuthor, r.createdBy.String)
	assert.False(t, r.keyID.Valid)
}

func checkAuthorlessRecordRefused(t *testing.T, svc *transaction.Service, tc tenant.Context, walletID uuid.UUID) {
	t.Helper()
	ctx := tenant.Into(t.Context(), tenant.Context{OrgID: tc.OrgID, ProjectID: tc.ProjectID})
	_, err := svc.Record(ctx, transaction.RecordInput{
		WalletID: walletID, Kind: transaction.KindIncome, Amount: money.New(1, money.IDR),
		OccurredAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
	})
	require.Error(t, err)
	assert.True(t, transaction.IsAuthorMissingError(err), "want AuthorMissingError, got %v", err)
}

func TestSQLite_Transaction_KeyAuthorLegacyNilAuthor(t *testing.T) {
	f := newSQLiteFixture(t)
	svc := newAuthorService(f.store, f.period,
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	id := uuid.New()
	const ts = "2026-08-05T12:00:00Z"
	mustExec(t, f.db,
		"INSERT INTO "+f.prefix+"transactions (id, org_id, project_id, wallet_id, kind, amount_minor, currency, period_id, occurred_at, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, 'income', 100, 'IDR', ?, ?, ?, ?, ?)",
		id.String(), f.tc.OrgID.String(), f.tc.ProjectID.String(), f.walletA.String(), f.period.String(), ts, nilAuthor, ts, ts)
	checkLegacyNilAuthorRevises(t, f.db, svc, f.tc, id,
		"SELECT created_by, created_by_key_id FROM "+f.prefix+"transactions WHERE id = ?")
	checkAuthorlessRecordRefused(t, svc, f.tc, f.walletA)
}
