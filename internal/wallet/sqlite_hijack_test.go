package wallet_test

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

// hijackFixture runs on SQLite deliberately: SQLite has no row level security, so the
// adapter's own tenant predicate is the only thing under test.
type hijackFixture struct {
	store wallet.Store
	orgA  tenant.Context
	orgB  tenant.Context
}

func newHijackFixture(t *testing.T) hijackFixture {
	t.Helper()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "hijack.db")

	sqlDB, err := db.Open(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	uidA, oidA, pidA := seedSQLiteTenant(t, sqlDB, cfg.DB.TablePrefix)
	uidB, oidB, pidB := seedSQLiteTenant(t, sqlDB, cfg.DB.TablePrefix)

	store := wallet.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	)
	return hijackFixture{
		store: store,
		orgA:  tenant.Context{OrgID: oidA, ProjectID: pidA, UserID: uidA},
		orgB:  tenant.Context{OrgID: oidB, ProjectID: pidB, UserID: uidB},
	}
}

func (f hijackFixture) seedVictim(t *testing.T) *wallet.Wallet {
	t.Helper()
	victim, err := wallet.New(f.orgA.OrgID, f.orgA.ProjectID, wallet.Params{
		Name: "org A's wallet", Kind: wallet.KindBank, Currency: money.IDR,
	})
	require.NoError(t, err)
	require.NoError(t, f.store.Save(tenant.Into(t.Context(), f.orgA), victim))
	return victim
}

func (f hijackFixture) requireName(t *testing.T, id uuid.UUID, want string) {
	t.Helper()
	got, err := f.store.ByID(tenant.Into(t.Context(), f.orgA), id)
	require.NoError(t, err)
	require.Equal(t, want, got.Name,
		"org A's row reads %q, want %q: another org rewrote it across the tenant boundary", got.Name, want)
}

func requireBlocked(t *testing.T, err error, what string) {
	t.Helper()
	require.Error(t, err, "%s: succeeded, want a cross-tenant write to be refused", what)
	require.True(t, wallet.IsNotFoundError(err), "%s: got %T: %v, want *wallet.NotFoundError", what, err, err)
}

// TestSQLiteStore_Save_RejectsReskinnedHijack covers the handler-shaped attack: the attacker's own org and project carrying the victim's row id.
func TestSQLiteStore_Save_RejectsReskinnedHijack(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	attack, err := wallet.New(f.orgB.OrgID, f.orgB.ProjectID, wallet.Params{
		Name: "Hijacked", Kind: wallet.KindCash, Currency: money.IDR,
	})
	require.NoError(t, err)
	attack.ID = victim.ID

	requireBlocked(t, f.store.Save(tenant.Into(t.Context(), f.orgB), attack), "reskinned Save")
	f.requireName(t, victim.ID, "org A's wallet")
}

// TestSQLiteStore_Save_RejectsVerbatimHijack covers the copied-row shape: the victim's org, project and row id replayed under the attacker's scope.
func TestSQLiteStore_Save_RejectsVerbatimHijack(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	attack := *victim
	attack.Name = "Hijacked"

	requireBlocked(t, f.store.Save(tenant.Into(t.Context(), f.orgB), &attack), "verbatim Save")
	f.requireName(t, victim.ID, "org A's wallet")
}

// TestSQLiteStore_Save_UpdatesOwnRow pins that the tenant predicate still lets the owning org through — a WHERE(false) guard would pass every hijack test without it.
func TestSQLiteStore_Save_UpdatesOwnRow(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	require.NoError(t, victim.Rename("renamed by its owner"))
	require.NoError(t, f.store.Save(tenant.Into(t.Context(), f.orgA), victim))
	f.requireName(t, victim.ID, "renamed by its owner")
}

// TestSQLiteStore_Delete_RejectsCrossTenant pins the delete path's org predicate.
func TestSQLiteStore_Delete_RejectsCrossTenant(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	requireBlocked(t, f.store.Delete(tenant.Into(t.Context(), f.orgB), victim.ID), "cross-tenant Delete")
	f.requireName(t, victim.ID, "org A's wallet")
}

// TestSQLiteStore_ByID_RejectsCrossTenant pins the read path's org predicate.
func TestSQLiteStore_ByID_RejectsCrossTenant(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	_, err := f.store.ByID(tenant.Into(t.Context(), f.orgB), victim.ID)
	assert.True(t, wallet.IsNotFoundError(err), "got %T: %v", err, err)
}
