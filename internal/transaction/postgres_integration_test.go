//go:build integration

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
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

type pgFixture struct {
	store  transaction.Store
	db     *sql.DB
	prefix string
	tc     tenant.Context

	walletA  uuid.UUID
	walletB  uuid.UUID
	category uuid.UUID
	period   uuid.UUID
}

func (f pgFixture) ctx(t *testing.T) context.Context {
	t.Helper()
	return tenant.Into(t.Context(), f.tc)
}

func newPgFixture(t *testing.T) pgFixture {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	userID, orgID, projID := seedPgTenant(t, sqlDB, prefix)

	f := pgFixture{
		db:     sqlDB,
		prefix: prefix,
		tc:     tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID},
	}
	f.walletA = seedPgWallet(t, sqlDB, prefix, orgID, projID, "Cash", "IDR")
	f.walletB = seedPgWallet(t, sqlDB, prefix, orgID, projID, "Bank", "IDR")
	f.category = seedPgCategory(t, sqlDB, prefix, orgID, projID, "Food", "expense")
	f.period = seedPgPeriod(t, sqlDB, prefix, orgID, projID, "August", "2026-08-01", "2026-08-31")

	pc := tenant.NewPgConn(sqlDB)
	f.store = transaction.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		pc,
	)
	return f
}

func seedPgTenant(t *testing.T, sqlDB *sql.DB, prefix string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
		userID, userID.String()+"@x.co", now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'Acme', $3, $4, $4)",
		orgID, orgID.String()[:8], userID, now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Web', $4, $5, $5)",
		projID, orgID, projID.String()[:8], userID, now)
	require.NoError(t, err)
	return userID, orgID, projID
}

func seedPgWallet(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, currency string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"wallets (id, org_id, project_id, name, kind, provider, currency, exclude_from_total, archived_at, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, 'cash', '', $5, false, NULL, $6, $6)",
		id, orgID, projID, name, currency, now)
	require.NoError(t, err)
	return id
}

func seedPgCategory(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, kind string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"categories (id, org_id, project_id, name, kind, icon, color, sort_order, archived_at, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $5, '', '', 0, NULL, $6, $6)",
		id, orgID, projID, name, kind, now)
	require.NoError(t, err)
	return id
}

func seedPgPeriod(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, start, end string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"periods (id, org_id, project_id, name, start_date, end_date, status, closed_at, snapshot, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $5::date, $6::date, 'open', NULL, NULL, $7, $7)",
		id, orgID, projID, name, start, end, now)
	require.NoError(t, err)
	return id
}

func (f pgFixture) save(t *testing.T, p transaction.NewParams) *transaction.Transaction {
	t.Helper()
	if p.CreatedBy == uuid.Nil {
		p.CreatedBy = f.tc.UserID
	}
	tx, err := transaction.New(f.tc.OrgID, f.tc.ProjectID, p)
	require.NoError(t, err)
	require.NoError(t, f.store.Save(f.ctx(t), tx))
	return tx
}

func TestPostgres_Transaction_RoundTripWithNullableColumns(t *testing.T) {
	f := newPgFixture(t)

	full := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(25_000, money.IDR),
		CategoryID: &f.category,
		PeriodID:   &f.period,
		Note:       "Kopi Kenangan",
		OccurredAt: time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC),
	})
	got, err := f.store.ByID(f.ctx(t), full.ID)
	require.NoError(t, err)
	require.NotNil(t, got.CategoryID)
	assert.Equal(t, f.category, *got.CategoryID)
	require.NotNil(t, got.PeriodID)
	assert.Equal(t, f.period, *got.PeriodID)
	assert.Nil(t, got.ToWalletID)
	assert.Equal(t, int64(25_000), got.Amount.Minor)
	assert.Equal(t, money.IDR, got.Amount.Currency)
	assert.True(t, got.OccurredAt.Equal(full.OccurredAt))
	assert.Equal(t, f.tc.UserID, got.CreatedBy)

	bare := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		ToWalletID: &f.walletB,
		Kind:       transaction.KindTransfer,
		Amount:     money.New(100_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	})
	gotBare, err := f.store.ByID(f.ctx(t), bare.ID)
	require.NoError(t, err)
	require.NotNil(t, gotBare.ToWalletID)
	assert.Equal(t, f.walletB, *gotBare.ToWalletID)
	assert.Nil(t, gotBare.CategoryID)
	assert.Nil(t, gotBare.PeriodID)
	assert.Empty(t, gotBare.Note)
}

func TestPostgres_Transaction_SaveWritesNoteNorm(t *testing.T) {
	f := newPgFixture(t)
	f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		Note:       "  Kopi   KENANGAN ",
		OccurredAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
	})
	var norm string
	require.NoError(t, f.db.QueryRowContext(t.Context(), "SELECT note_norm FROM "+f.prefix+"transactions").Scan(&norm))
	assert.Equal(t, "kopi kenangan", norm)
}

func TestPostgres_Transaction_NotFound(t *testing.T) {
	f := newPgFixture(t)
	_, err := f.store.ByID(f.ctx(t), uuid.New())
	assert.True(t, transaction.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

func TestPostgres_Transaction_KeysetPagination(t *testing.T) {
	f := newPgFixture(t)
	for day := 1; day <= 5; day++ {
		f.save(t, transaction.NewParams{
			WalletID:   f.walletA,
			Kind:       transaction.KindExpense,
			Amount:     money.New(int64(day)*1_000, money.IDR),
			OccurredAt: time.Date(2026, 8, day, 12, 0, 0, 0, time.UTC),
		})
	}
	seen := map[uuid.UUID]bool{}
	var after *transaction.Cursor
	for page, want := range []int{2, 2, 1} {
		rows, err := f.store.List(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Limit: 2, After: after})
		require.NoError(t, err, "page %d", page)
		require.Len(t, rows, want, "page %d", page)
		for i, r := range rows {
			require.False(t, seen[r.ID], "page %d: %s appeared twice", page, r.ID)
			seen[r.ID] = true
			if i > 0 {
				assert.True(t, rows[i-1].OccurredAt.After(r.OccurredAt), "page %d must be newest first", page)
			}
		}
		last := rows[len(rows)-1]
		after = &transaction.Cursor{OccurredAt: last.OccurredAt, CreatedAt: last.CreatedAt, ID: last.ID}
	}
	assert.Len(t, seen, 5)

	tail, err := f.store.List(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Limit: 2, After: after})
	require.NoError(t, err)
	assert.Empty(t, tail)
}

func TestPostgres_Transaction_KeysetPaginationTiesOnOccurredAt(t *testing.T) {
	f := newPgFixture(t)
	at := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	for range 4 {
		f.save(t, transaction.NewParams{
			WalletID:   f.walletA,
			Kind:       transaction.KindExpense,
			Amount:     money.New(1_000, money.IDR),
			OccurredAt: at,
		})
	}
	seen := map[uuid.UUID]bool{}
	var after *transaction.Cursor
	for range 2 {
		rows, err := f.store.List(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Limit: 2, After: after})
		require.NoError(t, err)
		require.Len(t, rows, 2, "ties on occurred_at must still page")
		for _, r := range rows {
			require.False(t, seen[r.ID], "%s appeared twice across pages with tied occurred_at", r.ID)
			seen[r.ID] = true
		}
		last := rows[len(rows)-1]
		after = &transaction.Cursor{OccurredAt: last.OccurredAt, CreatedAt: last.CreatedAt, ID: last.ID}
	}
	assert.Len(t, seen, 4)
}

func TestPostgres_Transaction_ListFilters(t *testing.T) {
	f := newPgFixture(t)
	income := seedPgCategory(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Salary", "income")
	july := seedPgPeriod(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "July", "2026-07-01", "2026-07-31")

	expense := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(25_000, money.IDR),
		CategoryID: &f.category,
		PeriodID:   &f.period,
		Note:       "Kopi Kenangan",
		OccurredAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
	})
	salary := f.save(t, transaction.NewParams{
		WalletID:   f.walletB,
		Kind:       transaction.KindIncome,
		Amount:     money.New(9_000_000, money.IDR),
		CategoryID: &income,
		PeriodID:   &july,
		Note:       "Gaji Juli",
		OccurredAt: time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	})
	move := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		ToWalletID: &f.walletB,
		Kind:       transaction.KindTransfer,
		Amount:     money.New(100_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
	})

	ids := func(opts transaction.ListOpts) map[uuid.UUID]bool {
		t.Helper()
		rows, err := f.store.List(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, opts)
		require.NoError(t, err)
		out := map[uuid.UUID]bool{}
		for _, r := range rows {
			out[r.ID] = true
		}
		return out
	}

	got := ids(transaction.ListOpts{WalletID: &f.walletB})
	assert.Equal(t, map[uuid.UUID]bool{salary.ID: true, move.ID: true}, got, "a wallet filter must cover both sides of a transfer")

	assert.Equal(t, map[uuid.UUID]bool{expense.ID: true}, ids(transaction.ListOpts{CategoryID: &f.category}))
	assert.Equal(t, map[uuid.UUID]bool{salary.ID: true}, ids(transaction.ListOpts{PeriodID: &july}))
	assert.Equal(t, map[uuid.UUID]bool{salary.ID: true, move.ID: true},
		ids(transaction.ListOpts{Kinds: []transaction.Kind{transaction.KindIncome, transaction.KindTransfer}}))

	from := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, map[uuid.UUID]bool{expense.ID: true, move.ID: true},
		ids(transaction.ListOpts{From: &from, To: &to}), "the date range must be inclusive on both ends")

	assert.Equal(t, map[uuid.UUID]bool{expense.ID: true},
		ids(transaction.ListOpts{Search: "KENANGAN"}), "search must be case-insensitive")
}

func TestPostgres_Transaction_Balances(t *testing.T) {
	f := newPgFixture(t)
	f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindOpening,
		Amount:     money.New(1_000_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(250_000, money.IDR),
		CategoryID: &f.category,
		OccurredAt: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
	})
	f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		ToWalletID: &f.walletB,
		Kind:       transaction.KindTransfer,
		Amount:     money.New(100_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
	})

	all, err := f.store.Balances(f.ctx(t), f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, int64(650_000), all[f.walletA].Minor)
	assert.Equal(t, int64(100_000), all[f.walletB].Minor, "the inflow side of a transfer must count")
	assert.Equal(t, money.IDR, all[f.walletA].Currency)

	one, err := f.store.Balance(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, f.walletB)
	require.NoError(t, err)
	assert.Equal(t, int64(100_000), one.Minor)
	assert.Equal(t, money.IDR, one.Currency)

	empty := seedPgWallet(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Unused", "IDR")
	zero, err := f.store.Balance(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, empty)
	require.NoError(t, err)
	assert.Equal(t, int64(0), zero.Minor)
}

func TestPostgres_Transaction_LastCategoryForNote(t *testing.T) {
	f := newPgFixture(t)
	newer := seedPgCategory(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Coffee", "expense")

	f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(20_000, money.IDR),
		CategoryID: &f.category,
		Note:       "Kopi Kenangan",
		OccurredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(22_000, money.IDR),
		CategoryID: &newer,
		Note:       "  kopi   KENANGAN ",
		OccurredAt: time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
	})

	got, ok, err := f.store.LastCategoryForNote(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, "kopi kenangan")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, newer, got, "the newest matching row wins")

	_, ok, err = f.store.LastCategoryForNote(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, "never seen")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestPostgres_Transaction_Delete(t *testing.T) {
	f := newPgFixture(t)
	tx := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, f.store.Delete(f.ctx(t), tx.ID))
	_, err := f.store.ByID(f.ctx(t), tx.ID)
	assert.True(t, transaction.IsNotFoundError(err))
	assert.True(t, transaction.IsNotFoundError(f.store.Delete(f.ctx(t), tx.ID)), "double delete")
}

func TestPostgres_Transaction_SaveIsUpsert(t *testing.T) {
	f := newPgFixture(t)
	tx := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		Note:       "before",
		OccurredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, tx.SetNote("after"))
	require.NoError(t, tx.SetCategory(&f.category))
	require.NoError(t, f.store.Save(f.ctx(t), tx))

	got, err := f.store.ByID(f.ctx(t), tx.ID)
	require.NoError(t, err)
	assert.Equal(t, "after", got.Note)
	require.NotNil(t, got.CategoryID)
	assert.Equal(t, f.category, *got.CategoryID)

	require.NoError(t, tx.SetCategory(nil))
	require.NoError(t, f.store.Save(f.ctx(t), tx))
	cleared, err := f.store.ByID(f.ctx(t), tx.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.CategoryID, "an upsert must be able to clear a nullable column")
}

// TestPostgres_Transaction_CompositeFKRefusesAnotherOrgsWallet proves the schema's (wallet_id, org_id) foreign key, not any Go-side check.
func TestPostgres_Transaction_CompositeFKRefusesAnotherOrgsWallet(t *testing.T) {
	f := newPgFixture(t)
	_, otherOrg, otherProj := seedPgTenant(t, f.db, f.prefix)
	foreignWallet := seedPgWallet(t, f.db, f.prefix, otherOrg, otherProj, "Their cash", "IDR")

	now := time.Now().UTC()
	_, err := f.db.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, "+
			"category_id, period_id, note, note_norm, occurred_at, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, NULL, 'expense', 1000, 'IDR', NULL, NULL, '', '', $5, $6, $5, $5)",
		uuid.New(), f.tc.OrgID, f.tc.ProjectID, foreignWallet, now, f.tc.UserID)
	require.Error(t, err, "a transaction naming another org's wallet must violate the composite foreign key")
}

// TestPostgres_Transaction_CompositeFKRefusesAnotherOrgsCategory pins the (category_id, org_id) foreign key.
func TestPostgres_Transaction_CompositeFKRefusesAnotherOrgsCategory(t *testing.T) {
	f := newPgFixture(t)
	_, otherOrg, otherProj := seedPgTenant(t, f.db, f.prefix)
	foreignCategory := seedPgCategory(t, f.db, f.prefix, otherOrg, otherProj, "Their food", "expense")

	now := time.Now().UTC()
	_, err := f.db.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, "+
			"category_id, period_id, note, note_norm, occurred_at, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, NULL, 'expense', 1000, 'IDR', $5, NULL, '', '', $6, $7, $6, $6)",
		uuid.New(), f.tc.OrgID, f.tc.ProjectID, f.walletA, foreignCategory, now, f.tc.UserID)
	require.Error(t, err, "a transaction naming another org's category must violate the composite foreign key")
}

func TestPostgres_Transaction_SaveTranslatesForeignKeyViolation(t *testing.T) {
	f := newPgFixture(t)
	tx, err := transaction.New(f.tc.OrgID, f.tc.ProjectID, transaction.NewParams{
		WalletID:   uuid.New(),
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		OccurredAt: time.Now(),
		CreatedBy:  f.tc.UserID,
	})
	require.NoError(t, err)
	err = f.store.Save(f.ctx(t), tx)
	assert.True(t, transaction.IsNotFoundError(err), "want NotFoundError for an unknown wallet, got %T: %v", err, err)
}

// TestPostgres_Transaction_RejectsCrossTenantWrites pins the org predicates on a BYPASSRLS connection, where RLS is not the guard.
func TestPostgres_Transaction_RejectsCrossTenantWrites(t *testing.T) {
	f := newPgFixture(t)
	victim := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(25_000, money.IDR),
		Note:       "org A's transaction",
		OccurredAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
	})

	otherUser, otherOrg, otherProj := seedPgTenant(t, f.db, f.prefix)
	ctxB := tenant.Into(t.Context(), tenant.Context{OrgID: otherOrg, ProjectID: otherProj, UserID: otherUser})

	attack := *victim
	attack.Note = "Hijacked"
	assert.True(t, transaction.IsNotFoundError(f.store.Save(ctxB, &attack)), "cross-tenant Save must be refused")
	assert.True(t, transaction.IsNotFoundError(f.store.Delete(ctxB, victim.ID)), "cross-tenant Delete must be refused")
	_, err := f.store.ByID(ctxB, victim.ID)
	assert.True(t, transaction.IsNotFoundError(err), "cross-tenant ByID must be refused")

	got, err := f.store.ByID(f.ctx(t), victim.ID)
	require.NoError(t, err)
	assert.Equal(t, "org A's transaction", got.Note, "another org rewrote the row across the tenant boundary")
}

func TestPostgres_Transaction_EnrollsInAnOuterUnitOfWork(t *testing.T) {
	f := newPgFixture(t)
	ctx := f.ctx(t)

	tx, err := transaction.New(f.tc.OrgID, f.tc.ProjectID, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:  f.tc.UserID,
	})
	require.NoError(t, err)

	pc := tenant.NewPgConn(f.db)
	require.NoError(t, tenant.RunInTx(ctx, pc, f.tc, func(inner context.Context) error {
		if sErr := f.store.Save(inner, tx); sErr != nil {
			return sErr
		}
		bal, bErr := f.store.Balance(inner, f.tc.OrgID, f.tc.ProjectID, f.walletA)
		if bErr != nil {
			return bErr
		}
		assert.Equal(t, int64(-1_000), bal.Minor, "the read must see the write of its own unit of work")
		return nil
	}))

	got, err := f.store.ByID(ctx, tx.ID)
	require.NoError(t, err)
	assert.Equal(t, tx.ID, got.ID)
}

func TestPostgres_Transaction_SearchTreatsWildcardsLiterally(t *testing.T) {
	f := newPgFixture(t)
	day := 1
	seed := func(note string) uuid.UUID {
		day++
		return f.save(t, transaction.NewParams{
			WalletID:   f.walletA,
			Kind:       transaction.KindExpense,
			Amount:     money.New(1_000, money.IDR),
			Note:       note,
			OccurredAt: time.Date(2026, 8, day, 0, 0, 0, 0, time.UTC),
		}).ID
	}
	percent := seed("50% off")
	underscore := seed("a_b")
	seed("50X off")
	seed("axb")

	for search, want := range map[string]uuid.UUID{"50%": percent, "a_b": underscore} {
		rows, err := f.store.List(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Search: search})
		require.NoError(t, err, "search %q", search)
		require.Len(t, rows, 1, "search %q must match only the literal text", search)
		assert.Equal(t, want, rows[0].ID)
	}
}

func TestPostgres_Transaction_LockWalletRequiresAUnitOfWork(t *testing.T) {
	f := newPgFixture(t)
	err := f.store.LockWallet(f.ctx(t), f.tc.OrgID, f.tc.ProjectID, f.walletA)
	assert.ErrorIs(t, err, transaction.ErrNoUnitOfWork,
		"an advisory lock outside a transaction is released at once, so this must fail loudly")
}

func TestPostgres_Transaction_LockWalletInsideAUnitOfWork(t *testing.T) {
	f := newPgFixture(t)
	pc := tenant.NewPgConn(f.db)
	require.NoError(t, tenant.RunInTx(f.ctx(t), pc, f.tc, func(inner context.Context) error {
		if err := f.store.LockWallet(inner, f.tc.OrgID, f.tc.ProjectID, f.walletA); err != nil {
			return err
		}
		return f.store.LockWallet(inner, f.tc.OrgID, f.tc.ProjectID, f.walletA)
	}), "the lock must be re-entrant within one transaction")
}

// TestPostgres_Transaction_LockWalletBlocksAConcurrentHolder proves the advisory lock actually excludes a second transaction, rather than being a no-op SELECT.
func TestPostgres_Transaction_LockWalletBlocksAConcurrentHolder(t *testing.T) {
	f := newPgFixture(t)
	pc := tenant.NewPgConn(f.db)

	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		holder := tenant.Into(context.Background(), f.tc)
		done <- tenant.RunInTx(holder, pc, f.tc, func(inner context.Context) error {
			if err := f.store.LockWallet(inner, f.tc.OrgID, f.tc.ProjectID, f.walletA); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-done:
		t.Fatalf("the holding transaction failed before it took the lock: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the first transaction to take the lock")
	}

	blocked, cancel := context.WithTimeout(f.ctx(t), 500*time.Millisecond)
	defer cancel()
	err := tenant.RunInTx(blocked, pc, f.tc, func(inner context.Context) error {
		return f.store.LockWallet(inner, f.tc.OrgID, f.tc.ProjectID, f.walletA)
	})
	require.Error(t, err, "a second transaction must not take the same wallet lock while the first holds it")
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"the second transaction must block on the lock until its deadline, not fail for another reason")

	close(release)
	require.NoError(t, <-done)

	free, cancelFree := context.WithTimeout(f.ctx(t), 5*time.Second)
	defer cancelFree()
	require.NoError(t, tenant.RunInTx(free, pc, f.tc, func(inner context.Context) error {
		return f.store.LockWallet(inner, f.tc.OrgID, f.tc.ProjectID, f.walletA)
	}), "the lock must be released when the holding transaction ends")
}

func TestPostgres_Transaction_AdjustThroughAServiceAndARealUnitOfWork(t *testing.T) {
	f := newPgFixture(t)
	pc := tenant.NewPgConn(f.db)
	uow := transaction.UnitOfWork(func(ctx context.Context, fn func(ctx context.Context) error) error {
		return tenant.RunInTx(ctx, pc, f.tc, fn)
	})
	periods := &periodStub{
		infos:     map[uuid.UUID]transaction.PeriodInfo{f.period: {ID: f.period}},
		byInstant: func(time.Time) (uuid.UUID, bool) { return f.period, true },
	}
	svc := transaction.NewService(f.store, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
			return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
				&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
		},
		transaction.WalletReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.WalletInfo, error) {
			return transaction.WalletInfo{ID: id, Currency: money.IDR}, nil
		}),
		transaction.CategoryReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.CategoryInfo, error) {
			return transaction.CategoryInfo{ID: id, Kind: "expense"}, nil
		}),
		periods, uow,
	)

	ctx := f.ctx(t)
	at := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	require.NoError(t, svc.RecordOpening(ctx, f.walletA, money.New(100_000, money.IDR), at, f.tc.UserID))

	got, err := svc.Adjust(ctx, f.walletA, money.New(150_000, money.IDR), at, f.tc.UserID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, transaction.KindAdjustmentIn, got.Kind)
	assert.Equal(t, int64(50_000), got.Amount.Minor)

	bal, err := svc.Balance(ctx, f.walletA)
	require.NoError(t, err)
	assert.Equal(t, int64(150_000), bal.Minor)

	again, err := svc.Adjust(ctx, f.walletA, money.New(150_000, money.IDR), at, f.tc.UserID)
	require.NoError(t, err)
	assert.Nil(t, again, "a second Adjust to the same target must write nothing")
}

// TestPostgres_Transaction_AdjustRefusesAPassThroughUnitOfWork pins that a misconfigured boot fails loudly instead of racing.
func TestPostgres_Transaction_AdjustRefusesAPassThroughUnitOfWork(t *testing.T) {
	f := newPgFixture(t)
	periods := &periodStub{
		infos:     map[uuid.UUID]transaction.PeriodInfo{f.period: {ID: f.period}},
		byInstant: func(time.Time) (uuid.UUID, bool) { return f.period, true },
	}
	svc := transaction.NewService(f.store, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
			return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
				&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
		},
		transaction.WalletReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.WalletInfo, error) {
			return transaction.WalletInfo{ID: id, Currency: money.IDR}, nil
		}),
		transaction.CategoryReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.CategoryInfo, error) {
			return transaction.CategoryInfo{ID: id, Kind: "expense"}, nil
		}),
		periods,
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	)
	_, err := svc.Adjust(f.ctx(t), f.walletA, money.New(150_000, money.IDR),
		time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), f.tc.UserID)
	assert.ErrorIs(t, err, transaction.ErrNoUnitOfWork)
}
