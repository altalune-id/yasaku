package transaction_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

type txnHijackFixture struct {
	store   transaction.Store
	db      *sql.DB
	prefix  string
	orgA    tenant.Context
	orgB    tenant.Context
	walletA uuid.UUID
	walletB uuid.UUID
}

func newTxnHijackFixture(t *testing.T) txnHijackFixture {
	t.Helper()
	ctx := context.Background()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "hijack.db")

	sqlDB, err := db.Open(ctx, cfg.DB, nil)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := schema.MigrateUp(ctx, sqlDB, cfg); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	prefix := cfg.DB.TablePrefix

	uidA, oidA, pidA := seedTxnTenant(t, sqlDB, prefix)
	uidB, oidB, pidB := seedTxnTenant(t, sqlDB, prefix)

	return txnHijackFixture{
		store:   transaction.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix}, db.Pool{W: sqlDB, R: sqlDB}, nil),
		db:      sqlDB,
		prefix:  prefix,
		orgA:    tenant.Context{OrgID: oidA, ProjectID: pidA, UserID: uidA},
		orgB:    tenant.Context{OrgID: oidB, ProjectID: pidB, UserID: uidB},
		walletA: seedWallet(t, sqlDB, prefix, oidA, pidA, "A cash", "IDR"),
		walletB: seedWallet(t, sqlDB, prefix, oidB, pidB, "B cash", "IDR"),
	}
}

func (f txnHijackFixture) seedVictim(t *testing.T) *transaction.Transaction {
	t.Helper()
	victim, err := transaction.New(f.orgA.OrgID, f.orgA.ProjectID, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(25_000, money.IDR),
		Note:       "org A's transaction",
		OccurredAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		CreatedBy:  f.orgA.UserID,
	})
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	if err := f.store.Save(tenant.Into(context.Background(), f.orgA), victim); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	return victim
}

func (f txnHijackFixture) requireNote(t *testing.T, id uuid.UUID, want string) {
	t.Helper()
	got, err := f.store.ByID(tenant.Into(context.Background(), f.orgA), id)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Note != want {
		t.Fatalf("org A's row reads %q, want %q: another org rewrote it across the tenant boundary", got.Note, want)
	}
}

func requireTxnBlocked(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: succeeded, want a cross-tenant write to be refused", what)
	}
	if !transaction.IsNotFoundError(err) {
		t.Fatalf("%s: got %T %v, want *transaction.NotFoundError", what, err, err)
	}
}

// TestSQLiteStore_Save_RejectsReskinnedHijack covers the handler-shaped attack: the attacker's own org and project carrying the victim's row id.
func TestSQLiteStore_Save_RejectsReskinnedHijack(t *testing.T) {
	f := newTxnHijackFixture(t)
	victim := f.seedVictim(t)

	// NOTE: org A's wallet, so the composite (wallet_id, org_id) foreign key stays satisfied and the tenant predicate is the only thing left to refuse the write.
	attack, err := transaction.New(f.orgB.OrgID, f.orgB.ProjectID, transaction.NewParams{
		WalletID:   f.walletA,
		Kind:       transaction.KindExpense,
		Amount:     money.New(1, money.IDR),
		Note:       "Hijacked",
		OccurredAt: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		CreatedBy:  f.orgB.UserID,
	})
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	attack.ID = victim.ID

	requireTxnBlocked(t, f.store.Save(tenant.Into(context.Background(), f.orgB), attack), "reskinned Save")
	f.requireNote(t, victim.ID, "org A's transaction")
}

// TestSQLiteStore_Save_RejectsVerbatimHijack covers the copied-row shape: the victim's org, project and row id replayed under the attacker's scope.
func TestSQLiteStore_Save_RejectsVerbatimHijack(t *testing.T) {
	f := newTxnHijackFixture(t)
	victim := f.seedVictim(t)

	attack := *victim
	attack.Note = "Hijacked"

	requireTxnBlocked(t, f.store.Save(tenant.Into(context.Background(), f.orgB), &attack), "verbatim Save")
	f.requireNote(t, victim.ID, "org A's transaction")
}

// TestSQLiteStore_Save_UpdatesOwnRow pins that the tenant predicate still lets the owning org through — a WHERE(false) guard would pass every hijack test without it.
func TestSQLiteStore_Save_UpdatesOwnRow(t *testing.T) {
	f := newTxnHijackFixture(t)
	victim := f.seedVictim(t)

	victim.Note = "renamed by its owner"
	if err := f.store.Save(tenant.Into(context.Background(), f.orgA), victim); err != nil {
		t.Fatalf("owner Save: %v", err)
	}
	f.requireNote(t, victim.ID, "renamed by its owner")
}

// TestSQLiteStore_Delete_RejectsCrossTenant pins the delete path's org predicate.
func TestSQLiteStore_Delete_RejectsCrossTenant(t *testing.T) {
	f := newTxnHijackFixture(t)
	victim := f.seedVictim(t)

	requireTxnBlocked(t, f.store.Delete(tenant.Into(context.Background(), f.orgB), victim.ID), "cross-tenant Delete")
	f.requireNote(t, victim.ID, "org A's transaction")
}

// TestSQLiteStore_ByID_RejectsCrossTenant pins the read path's org predicate.
func TestSQLiteStore_ByID_RejectsCrossTenant(t *testing.T) {
	f := newTxnHijackFixture(t)
	victim := f.seedVictim(t)

	_, err := f.store.ByID(tenant.Into(context.Background(), f.orgB), victim.ID)
	requireTxnBlocked(t, err, "cross-tenant ByID")
}

// TestSQLiteStore_Reads_RejectCrossTenant pins that the aggregate reads carry the org predicate too.
func TestSQLiteStore_Reads_RejectCrossTenant(t *testing.T) {
	f := newTxnHijackFixture(t)
	victim := f.seedVictim(t)
	ctxB := tenant.Into(context.Background(), f.orgB)

	rows, err := f.store.List(ctxB, f.orgA.OrgID, f.orgA.ProjectID, transaction.ListOpts{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("org B listed %d of org A's rows", len(rows))
	}

	balances, err := f.store.Balances(ctxB, f.orgA.OrgID, f.orgA.ProjectID)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if len(balances) != 0 {
		t.Errorf("org B read org A's balances: %v", balances)
	}

	bal, err := f.store.Balance(ctxB, f.orgA.OrgID, f.orgA.ProjectID, victim.WalletID)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if bal.Minor != 0 {
		t.Errorf("org B read org A's wallet balance: %v", bal)
	}

	if _, ok, err := f.store.LastCategoryForNote(ctxB, f.orgA.OrgID, f.orgA.ProjectID, "org a's transaction"); err != nil || ok {
		t.Errorf("org B read org A's note history: ok=%v err=%v", ok, err)
	}
}
