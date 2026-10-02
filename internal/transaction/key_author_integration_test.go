//go:build integration

package transaction_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

func TestPostgres_Transaction_KeyAuthor(t *testing.T) {
	f := newPgFixture(t)
	keyID := uuid.New()
	_, err := f.db.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"api_keys (id, org_id, project_id, name, secret_hash, created_at) VALUES ($1, $2, $3, 'ci', '\\x01', $4)",
		keyID, f.tc.OrgID, f.tc.ProjectID, time.Now().UTC())
	require.NoError(t, err)
	pc := tenant.NewPgConn(f.db)
	svc := newAuthorService(f.store, f.period, func(ctx context.Context, fn func(context.Context) error) error {
		tc, tErr := tenant.From(ctx)
		if tErr != nil {
			return tErr
		}
		return tenant.RunInTx(ctx, pc, tc, fn)
	})
	query := "SELECT created_by::text, created_by_key_id::text FROM " + f.prefix + "transactions WHERE id = $1"
	at := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	byKey, err := svc.Record(keyPrincipalCtx(t.Context(), f.tc, keyID), transaction.RecordInput{
		WalletID: f.walletA, Kind: transaction.KindIncome, Amount: money.New(1_000, money.IDR), OccurredAt: at,
	})
	require.NoError(t, err)
	assert.Equal(t, keyID, byKey.CreatedByKeyID)
	assertKeyAuthored(t, readTxnAuthor(t, f.db, query, byKey.ID), keyID)

	got, err := f.store.ByID(f.ctx(t), byKey.ID)
	require.NoError(t, err)
	assert.Equal(t, keyID, got.CreatedByKeyID, "the store must read the key author back")
	assert.Equal(t, uuid.Nil, got.CreatedBy)

	adj, err := svc.Adjust(keyPrincipalCtx(t.Context(), f.tc, keyID), f.walletB, money.New(5_000, money.IDR), at, uuid.Nil)
	require.NoError(t, err)
	require.NotNil(t, adj)
	assertKeyAuthored(t, readTxnAuthor(t, f.db, query, adj.ID), keyID)

	byPerson, err := svc.Record(personCtx(t.Context(), f.tc), transaction.RecordInput{
		WalletID: f.walletA, Kind: transaction.KindIncome, Amount: money.New(2_000, money.IDR), OccurredAt: at,
	})
	require.NoError(t, err)
	assertPersonAuthored(t, readTxnAuthor(t, f.db, query, byPerson.ID), f.tc.UserID)

	_, err = f.db.ExecContext(t.Context(),
		"UPDATE "+f.prefix+"transactions SET created_by = $1 WHERE id = $2", f.tc.UserID, byKey.ID)
	require.Error(t, err, "a row naming both a user and a key must be refused")
}

func TestPostgres_Transaction_KeyAuthorLegacyNilAuthor(t *testing.T) {
	f := newPgFixture(t)
	svc := newAuthorService(f.store, f.period,
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	id := uuid.New()
	ts := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	_, err := f.db.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"transactions (id, org_id, project_id, wallet_id, kind, amount_minor, currency, period_id, occurred_at, created_by, created_at, updated_at) VALUES ($1, $2, $3, $4, 'income', 100, 'IDR', $5, $6, $7, $6, $6)",
		id, f.tc.OrgID, f.tc.ProjectID, f.walletA, f.period, ts, uuid.Nil)
	require.NoError(t, err)
	checkLegacyNilAuthorRevises(t, f.db, svc, f.tc, id,
		"SELECT created_by::text, created_by_key_id::text FROM "+f.prefix+"transactions WHERE id = $1")
	checkAuthorlessRecordRefused(t, svc, f.tc, f.walletA)
}
