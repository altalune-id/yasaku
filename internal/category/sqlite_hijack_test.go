package category_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/schema"
)

// hijackFixture puts two tenants on one SQLite database. SQLite has no row level security,
// so the store's own org predicates are the only thing standing between org B and org A's
// rows — which is what makes these tests able to detect their removal.
type hijackFixture struct {
	store category.Store
	sqlDB *sql.DB
	orgA  tenant.Context
	orgB  tenant.Context
}

func newHijackFixture(t *testing.T) hijackFixture {
	t.Helper()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "category_hijack.db")

	sqlDB, err := db.Open(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	store := category.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	)
	return hijackFixture{
		store: store,
		sqlDB: sqlDB,
		orgA:  seedTenant(t, sqlDB, prefix),
		orgB:  seedTenant(t, sqlDB, prefix),
	}
}

func (f hijackFixture) seedVictim(t *testing.T) *category.Category {
	t.Helper()
	victim, err := category.New(f.orgA.OrgID, f.orgA.ProjectID, "org A's category", category.KindExpense, "", "", 0)
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
	require.Equal(t, f.orgA.OrgID, got.OrgID, "org A's row must still belong to org A")
}

// TestSQLite_Save_RejectsReskinnedHijack covers the handler-shaped attack: the attacker's own
// org and project on the struct, carrying the victim's row id.
func TestSQLite_Save_RejectsReskinnedHijack(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	attack, err := category.New(f.orgB.OrgID, f.orgB.ProjectID, "Hijacked", category.KindExpense, "", "", 0)
	require.NoError(t, err)
	attack.ID = victim.ID

	err = f.store.Save(tenant.Into(t.Context(), f.orgB), attack)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
	f.requireName(t, victim.ID, "org A's category")
}

// TestSQLite_Save_RejectsVerbatimHijack covers the copied-row shape: the victim's org, project
// and row id replayed under the attacker's tenant scope.
func TestSQLite_Save_RejectsVerbatimHijack(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	attack := *victim
	attack.Name = "Hijacked"

	err := f.store.Save(tenant.Into(t.Context(), f.orgB), &attack)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
	f.requireName(t, victim.ID, "org A's category")
}

// TestSQLite_Save_UpdatesOwnRow pins the other direction: a WHERE(false) guard would pass
// every hijack test above without it.
func TestSQLite_Save_UpdatesOwnRow(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	require.NoError(t, victim.Rename("renamed by its owner"))
	require.NoError(t, f.store.Save(tenant.Into(t.Context(), f.orgA), victim))
	f.requireName(t, victim.ID, "renamed by its owner")
}

// TestSQLite_Delete_RejectsCrossTenant pins the delete path's org predicate.
func TestSQLite_Delete_RejectsCrossTenant(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	err := f.store.Delete(tenant.Into(t.Context(), f.orgB), victim.ID)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
	f.requireName(t, victim.ID, "org A's category")
}

// TestSQLite_Reads_RejectCrossTenant pins the org predicate on every read path.
func TestSQLite_Reads_RejectCrossTenant(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)
	otherCtx := tenant.Into(t.Context(), f.orgB)

	_, err := f.store.ByID(otherCtx, victim.ID)
	assert.True(t, category.IsNotFoundError(err), "org B must not read org A's row")

	byIDs, err := f.store.ByIDs(otherCtx, f.orgA.OrgID, f.orgA.ProjectID, []uuid.UUID{victim.ID})
	require.NoError(t, err)
	assert.Empty(t, byIDs, "org B must not read org A's rows even when it names org A's scope")

	listed, err := f.store.List(otherCtx, f.orgA.OrgID, f.orgA.ProjectID, category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, listed, "org B must not list org A's rows even when it names org A's scope")
}
