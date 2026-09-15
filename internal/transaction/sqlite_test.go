package transaction_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

type sqliteFixture struct {
	store  transaction.Store
	db     *sql.DB
	prefix string
	tc     tenant.Context

	walletA  uuid.UUID
	walletB  uuid.UUID
	category uuid.UUID
	period   uuid.UUID
}

func (f sqliteFixture) ctx() context.Context {
	return tenant.Into(context.Background(), f.tc)
}

func newSQLiteFixture(t *testing.T) sqliteFixture {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		t.Fatalf("foreign_keys pragma: %v", err)
	}
	cfg := config.Defaults()
	if err := schema.MigrateUp(context.Background(), sqlDB, cfg); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	prefix := cfg.DB.TablePrefix

	userID, orgID, projID := seedTxnTenant(t, sqlDB, prefix)
	f := sqliteFixture{
		db:     sqlDB,
		prefix: prefix,
		tc:     tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID},
	}
	f.walletA = seedWallet(t, sqlDB, prefix, orgID, projID, "Cash", "IDR")
	f.walletB = seedWallet(t, sqlDB, prefix, orgID, projID, "Bank", "IDR")
	f.category = seedCategory(t, sqlDB, prefix, orgID, projID, "Food", "expense")
	f.period = seedPeriod(t, sqlDB, prefix, orgID, projID, "August", "2026-08-01", "2026-08-31")

	f.store = transaction.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	)
	return f
}

func seedTxnTenant(t *testing.T, sqlDB *sql.DB, prefix string) (userID, orgID, projID uuid.UUID) { //nolint:nonamedreturns // triple
	t.Helper()
	userID, orgID, projID = uuid.New(), uuid.New(), uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
		userID.String(), userID.String()+"@x.com", now, now)
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Org', ?, ?, ?)",
		orgID.String(), orgID.String()[:8], userID.String(), now, now)
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Web', ?, ?, ?)",
		projID.String(), orgID.String(), projID.String()[:8], userID.String(), now, now)
	return userID, orgID, projID
}

func seedWallet(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, currency string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"wallets (id, org_id, project_id, name, kind, provider, currency, exclude_from_total, archived_at, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, 'cash', '', ?, 0, NULL, ?, ?)",
		id.String(), orgID.String(), projID.String(), name, currency, now, now)
	return id
}

func seedCategory(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, kind string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"categories (id, org_id, project_id, name, kind, icon, color, sort_order, archived_at, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, '', '', 0, NULL, ?, ?)",
		id.String(), orgID.String(), projID.String(), name, kind, now, now)
	return id
}

func seedPeriod(t *testing.T, sqlDB *sql.DB, prefix string, orgID, projID uuid.UUID, name, start, end string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	mustExec(t, sqlDB,
		"INSERT INTO "+prefix+"periods (id, org_id, project_id, name, start_date, end_date, status, closed_at, snapshot, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, 'open', NULL, NULL, ?, ?)",
		id.String(), orgID.String(), projID.String(), name, start, end, now, now)
	return id
}

func mustExec(t *testing.T, sqlDB *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := sqlDB.Exec(query, args...); err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
}

func (f sqliteFixture) save(t *testing.T, p transaction.NewParams) *transaction.Transaction {
	t.Helper()
	if p.CreatedBy == uuid.Nil {
		p.CreatedBy = f.tc.UserID
	}
	tx, err := transaction.New(f.tc.OrgID, f.tc.ProjectID, p)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	if err := f.store.Save(f.ctx(), tx); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return tx
}

func TestSQLiteStore_RoundTripWithNullableColumns(t *testing.T) {
	f := newSQLiteFixture(t)

	full := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(25_000, money.IDR),
		CategoryID: &f.category,
		PeriodID:   &f.period,
		Note:       "  Kopi   Kenangan ",
		OccurredAt: time.Date(2026, 8, 10, 9, 30, 0, 123456789, time.UTC),
	})

	got, err := f.store.ByID(f.ctx(), full.ID)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.CategoryID == nil || *got.CategoryID != f.category {
		t.Errorf("category=%v", got.CategoryID)
	}
	if got.PeriodID == nil || *got.PeriodID != f.period {
		t.Errorf("period=%v", got.PeriodID)
	}
	if got.ToWalletID != nil {
		t.Errorf("to_wallet_id=%v want nil", got.ToWalletID)
	}
	if got.Note != "Kopi   Kenangan" {
		t.Errorf("note=%q", got.Note)
	}
	if !got.OccurredAt.Equal(full.OccurredAt) {
		t.Errorf("occurred_at=%v want %v", got.OccurredAt, full.OccurredAt)
	}
	if got.Amount.Minor != 25_000 || got.Amount.Currency != money.IDR {
		t.Errorf("amount=%v", got.Amount)
	}
	if got.CreatedBy != f.tc.UserID {
		t.Errorf("created_by=%v", got.CreatedBy)
	}

	bare := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		ToWalletID: &f.walletB,
		Kind:       transaction.KindTransfer,
		Amount:     money.New(100_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	})
	gotBare, err := f.store.ByID(f.ctx(), bare.ID)
	if err != nil {
		t.Fatalf("ByID transfer: %v", err)
	}
	if gotBare.ToWalletID == nil || *gotBare.ToWalletID != f.walletB {
		t.Errorf("to_wallet_id=%v", gotBare.ToWalletID)
	}
	if gotBare.CategoryID != nil || gotBare.PeriodID != nil {
		t.Errorf("nullable columns must read back as nil: %v %v", gotBare.CategoryID, gotBare.PeriodID)
	}
	if gotBare.Note != "" {
		t.Errorf("note=%q want empty", gotBare.Note)
	}
}

func TestSQLiteStore_SaveWritesNoteNorm(t *testing.T) {
	f := newSQLiteFixture(t)
	f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		Note:       "  Kopi   KENANGAN ",
		OccurredAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
	})
	var norm string
	if err := f.db.QueryRow("SELECT note_norm FROM " + f.prefix + "transactions").Scan(&norm); err != nil {
		t.Fatal(err)
	}
	if norm != "kopi kenangan" {
		t.Errorf("note_norm=%q want %q", norm, "kopi kenangan")
	}
}

func TestSQLiteStore_KeysetPagination(t *testing.T) {
	f := newSQLiteFixture(t)
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
	wantSizes := []int{2, 2, 1}
	for page, want := range wantSizes {
		rows, err := f.store.List(f.ctx(), f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Limit: 2, After: after})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(rows) != want {
			t.Fatalf("page %d size=%d want %d", page, len(rows), want)
		}
		for i, r := range rows {
			if seen[r.ID] {
				t.Fatalf("page %d row %d: %s appeared twice", page, i, r.ID)
			}
			seen[r.ID] = true
			if i > 0 && !rows[i-1].OccurredAt.After(r.OccurredAt) {
				t.Errorf("page %d is not ordered newest first", page)
			}
		}
		last := rows[len(rows)-1]
		after = &transaction.Cursor{OccurredAt: last.OccurredAt, CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(seen) != 5 {
		t.Errorf("saw %d distinct rows want 5", len(seen))
	}

	tail, err := f.store.List(f.ctx(), f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Limit: 2, After: after})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 {
		t.Errorf("past the end: %d rows want 0", len(tail))
	}
}

func TestSQLiteStore_KeysetPaginationTiesOnOccurredAt(t *testing.T) {
	f := newSQLiteFixture(t)
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
		rows, err := f.store.List(f.ctx(), f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Limit: 2, After: after})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows=%d want 2; ties on occurred_at must still page", len(rows))
		}
		for _, r := range rows {
			if seen[r.ID] {
				t.Fatalf("%s appeared twice across pages with tied occurred_at", r.ID)
			}
			seen[r.ID] = true
		}
		last := rows[len(rows)-1]
		after = &transaction.Cursor{OccurredAt: last.OccurredAt, CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(seen) != 4 {
		t.Errorf("saw %d want 4", len(seen))
	}
}

func TestSQLiteStore_ListFilters(t *testing.T) {
	f := newSQLiteFixture(t)
	income := seedCategory(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Salary", "income")
	other := seedPeriod(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "July", "2026-07-01", "2026-07-31")

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
		PeriodID:   &other,
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

	list := func(opts transaction.ListOpts) []*transaction.Transaction {
		t.Helper()
		rows, err := f.store.List(f.ctx(), f.tc.OrgID, f.tc.ProjectID, opts)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		return rows
	}
	ids := func(rows []*transaction.Transaction) map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		for _, r := range rows {
			out[r.ID] = true
		}
		return out
	}

	t.Run("wallet covers both sides of a transfer", func(t *testing.T) {
		got := ids(list(transaction.ListOpts{WalletID: &f.walletB}))
		if !got[salary.ID] || !got[move.ID] || got[expense.ID] || len(got) != 2 {
			t.Errorf("wallet filter: %v", got)
		}
	})
	t.Run("category", func(t *testing.T) {
		got := ids(list(transaction.ListOpts{CategoryID: &f.category}))
		if len(got) != 1 || !got[expense.ID] {
			t.Errorf("category filter: %v", got)
		}
	})
	t.Run("period", func(t *testing.T) {
		got := ids(list(transaction.ListOpts{PeriodID: &other}))
		if len(got) != 1 || !got[salary.ID] {
			t.Errorf("period filter: %v", got)
		}
	})
	t.Run("kinds", func(t *testing.T) {
		got := ids(list(transaction.ListOpts{Kinds: []transaction.Kind{transaction.KindIncome, transaction.KindTransfer}}))
		if len(got) != 2 || !got[salary.ID] || !got[move.ID] {
			t.Errorf("kind filter: %v", got)
		}
	})
	t.Run("date range is inclusive on both ends", func(t *testing.T) {
		from := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
		got := ids(list(transaction.ListOpts{From: &from, To: &to}))
		if len(got) != 2 || !got[expense.ID] || !got[move.ID] {
			t.Errorf("range filter: %v", got)
		}
	})
	t.Run("search is case-insensitive", func(t *testing.T) {
		got := ids(list(transaction.ListOpts{Search: "KENANGAN"}))
		if len(got) != 1 || !got[expense.ID] {
			t.Errorf("search filter: %v", got)
		}
	})
}

func TestSQLiteStore_Balances(t *testing.T) {
	f := newSQLiteFixture(t)
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

	all, err := f.store.Balances(f.ctx(), f.tc.OrgID, f.tc.ProjectID)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if all[f.walletA].Minor != 650_000 {
		t.Errorf("walletA=%v want 650000", all[f.walletA])
	}
	if all[f.walletB].Minor != 100_000 {
		t.Errorf("walletB=%v want 100000; the inflow side of a transfer must count", all[f.walletB])
	}
	if all[f.walletA].Currency != money.IDR {
		t.Errorf("currency=%q", all[f.walletA].Currency)
	}

	one, err := f.store.Balance(f.ctx(), f.tc.OrgID, f.tc.ProjectID, f.walletB)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if one.Minor != 100_000 || one.Currency != money.IDR {
		t.Errorf("Balance=%v want IDR 100000", one)
	}

	empty := seedWallet(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Unused", "IDR")
	zero, err := f.store.Balance(f.ctx(), f.tc.OrgID, f.tc.ProjectID, empty)
	if err != nil {
		t.Fatalf("Balance of an empty wallet: %v", err)
	}
	if zero.Minor != 0 {
		t.Errorf("empty wallet balance=%v want 0", zero)
	}
}

func TestSQLiteStore_LastCategoryForNote(t *testing.T) {
	f := newSQLiteFixture(t)
	newer := seedCategory(t, f.db, f.prefix, f.tc.OrgID, f.tc.ProjectID, "Coffee", "expense")

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

	got, ok, err := f.store.LastCategoryForNote(f.ctx(), f.tc.OrgID, f.tc.ProjectID, "kopi kenangan")
	if err != nil {
		t.Fatalf("LastCategoryForNote: %v", err)
	}
	if !ok || got != newer {
		t.Errorf("got=%v ok=%v want the newest matching row's category %v", got, ok, newer)
	}

	if _, ok, err := f.store.LastCategoryForNote(f.ctx(), f.tc.OrgID, f.tc.ProjectID, "never seen"); err != nil || ok {
		t.Errorf("unknown note: ok=%v err=%v", ok, err)
	}
}

func TestSQLiteStore_Delete(t *testing.T) {
	f := newSQLiteFixture(t)
	tx := f.save(t, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	})
	if err := f.store.Delete(f.ctx(), tx.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.store.ByID(f.ctx(), tx.ID); !transaction.IsNotFoundError(err) {
		t.Errorf("after Delete: %v", err)
	}
	if err := f.store.Delete(f.ctx(), tx.ID); !transaction.IsNotFoundError(err) {
		t.Errorf("double Delete: %v", err)
	}
}

func TestSQLiteStore_TenantMissing(t *testing.T) {
	f := newSQLiteFixture(t)
	tx, err := transaction.New(f.tc.OrgID, f.tc.ProjectID, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		OccurredAt: time.Now(),
		CreatedBy:  f.tc.UserID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Save(context.Background(), tx); !tenant.IsMissingError(err) {
		t.Errorf("Save without tenant: want MissingError, got %v", err)
	}
}

// TestSQLiteStore_CompositeFKRefusesAnotherOrgsWallet proves the schema's (wallet_id, org_id) foreign key, not any Go-side check.
func TestSQLiteStore_CompositeFKRefusesAnotherOrgsWallet(t *testing.T) {
	f := newSQLiteFixture(t)
	_, otherOrg, otherProj := seedTxnTenant(t, f.db, f.prefix)
	foreignWallet := seedWallet(t, f.db, f.prefix, otherOrg, otherProj, "Their cash", "IDR")

	now := sqliteent.SQLiteTime(time.Now())
	_, err := f.db.Exec(
		"INSERT INTO "+f.prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, "+
			"category_id, period_id, note, note_norm, occurred_at, created_by, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, NULL, 'expense', 1000, 'IDR', NULL, NULL, '', '', ?, ?, ?, ?)",
		uuid.New().String(), f.tc.OrgID.String(), f.tc.ProjectID.String(), foreignWallet.String(),
		now, f.tc.UserID.String(), now, now)
	if err == nil {
		t.Fatal("a transaction naming another org's wallet must violate the composite foreign key")
	}
}

// TestSQLiteStore_CompositeFKRefusesAnotherOrgsCategory pins the (category_id, org_id) foreign key.
func TestSQLiteStore_CompositeFKRefusesAnotherOrgsCategory(t *testing.T) {
	f := newSQLiteFixture(t)
	_, otherOrg, otherProj := seedTxnTenant(t, f.db, f.prefix)
	foreignCategory := seedCategory(t, f.db, f.prefix, otherOrg, otherProj, "Their food", "expense")

	now := sqliteent.SQLiteTime(time.Now())
	_, err := f.db.Exec(
		"INSERT INTO "+f.prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, "+
			"category_id, period_id, note, note_norm, occurred_at, created_by, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, NULL, 'expense', 1000, 'IDR', ?, NULL, '', '', ?, ?, ?, ?)",
		uuid.New().String(), f.tc.OrgID.String(), f.tc.ProjectID.String(), f.walletA.String(), foreignCategory.String(),
		now, f.tc.UserID.String(), now, now)
	if err == nil {
		t.Fatal("a transaction naming another org's category must violate the composite foreign key")
	}
}

func TestSQLiteStore_SaveTranslatesForeignKeyViolation(t *testing.T) {
	f := newSQLiteFixture(t)
	tx, err := transaction.New(f.tc.OrgID, f.tc.ProjectID, transaction.NewParams{
		WalletID:   uuid.New(),
		Kind:       transaction.KindExpense,
		Amount:     money.New(1_000, money.IDR),
		OccurredAt: time.Now(),
		CreatedBy:  f.tc.UserID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Save(f.ctx(), tx); !transaction.IsNotFoundError(err) {
		t.Errorf("unknown wallet: want IsNotFoundError, got %T: %v", err, err)
	}
}

func TestSQLiteStore_SearchTreatsWildcardsLiterally(t *testing.T) {
	f := newSQLiteFixture(t)
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
		rows, err := f.store.List(f.ctx(), f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Search: search})
		if err != nil {
			t.Fatalf("search %q: %v", search, err)
		}
		if len(rows) != 1 || rows[0].ID != want {
			t.Errorf("search %q matched %d rows, want exactly the literal match", search, len(rows))
		}
	}
}

func TestSQLiteStore_LockWalletRequiresAUnitOfWork(t *testing.T) {
	f := newSQLiteFixture(t)
	err := f.store.LockWallet(f.ctx(), f.tc.OrgID, f.tc.ProjectID, f.walletA)
	if !errors.Is(err, transaction.ErrNoUnitOfWork) {
		t.Errorf("want ErrNoUnitOfWork, got %v", err)
	}
}

func TestSQLiteStore_LockWalletInsideAUnitOfWork(t *testing.T) {
	f := newSQLiteFixture(t)
	pool := db.Pool{W: f.db, R: f.db}
	err := db.RunInTx(f.ctx(), pool, func(inner context.Context) error {
		return f.store.LockWallet(inner, f.tc.OrgID, f.tc.ProjectID, f.walletA)
	})
	if err != nil {
		t.Errorf("LockWallet inside a unit of work: %v", err)
	}
}
