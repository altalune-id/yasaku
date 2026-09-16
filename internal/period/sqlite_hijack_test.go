package period_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

type hijackFixture struct {
	store period.Store
	orgA  tenant.Context
	orgB  tenant.Context
}

// newHijackFixture runs on SQLite deliberately: SQLite has no RLS, so the adapter's own tenant predicate is the only guard under test.
func newHijackFixture(t *testing.T) hijackFixture {
	t.Helper()
	sqlDB, cfg := newSQLiteDB(t)
	orgA := seedTenant(t, sqlDB, cfg.DB.TablePrefix)
	orgB := seedTenant(t, sqlDB, cfg.DB.TablePrefix)
	store := period.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix},
		db.Pool{W: sqlDB, R: sqlDB}, nil)
	return hijackFixture{store: store, orgA: orgA, orgB: orgB}
}

func (f hijackFixture) seedVictim(t *testing.T) *period.Period {
	t.Helper()
	victim, err := period.New(f.orgA.OrgID, f.orgA.ProjectID, civil.Date{Year: 2026, Month: 8, Day: 25}, "org A's period")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(tenant.Into(t.Context(), f.orgA), victim))
	return victim
}

func (f hijackFixture) requireName(t *testing.T, id uuid.UUID, want string) {
	t.Helper()
	got, err := f.store.ByID(tenant.Into(t.Context(), f.orgA), id)
	require.NoError(t, err)
	assert.Equal(t, want, got.Name, "org A's row was rewritten across the tenant boundary")
}

func requireBlocked(t *testing.T, err error, what string) {
	t.Helper()
	require.Error(t, err, "%s: succeeded, want a cross-tenant write to be refused", what)
	assert.True(t, period.IsNotFoundError(err), "%s: got %T: %v", what, err, err)
}

// TestSQLiteStore_Save_RejectsReskinnedHijack covers the handler-shaped attack: the attacker's own org and project carrying the victim's row id.
func TestSQLiteStore_Save_RejectsReskinnedHijack(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	attack, err := period.New(f.orgB.OrgID, f.orgB.ProjectID, civil.Date{Year: 2026, Month: 8, Day: 25}, "Hijacked")
	require.NoError(t, err)
	attack.ID = victim.ID

	requireBlocked(t, f.store.Save(tenant.Into(t.Context(), f.orgB), attack), "reskinned Save")
	f.requireName(t, victim.ID, "org A's period")
}

// TestSQLiteStore_Save_RejectsVerbatimHijack covers the copied-row shape: the victim's org, project and row id replayed under the attacker's scope.
func TestSQLiteStore_Save_RejectsVerbatimHijack(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	attack := *victim
	attack.Name = "Hijacked"
	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	attack.EndDate = &end
	attack.Status = period.StatusClosed
	closedAt := time.Now().UTC()
	attack.ClosedAt = &closedAt

	requireBlocked(t, f.store.Save(tenant.Into(t.Context(), f.orgB), &attack), "verbatim Save")
	f.requireName(t, victim.ID, "org A's period")

	got, err := f.store.ByID(tenant.Into(t.Context(), f.orgA), victim.ID)
	require.NoError(t, err)
	assert.True(t, got.IsCurrent(), "the victim's period must not be closed by another org")
}

// TestSQLiteStore_Save_UpdatesOwnRow pins that the tenant predicate still lets the owning org through — a WHERE(false) guard would pass every hijack test without it.
func TestSQLiteStore_Save_UpdatesOwnRow(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	require.NoError(t, victim.Rename("renamed by its owner"))
	require.NoError(t, f.store.Save(tenant.Into(t.Context(), f.orgA), victim))
	f.requireName(t, victim.ID, "renamed by its owner")
}

// TestSQLiteStore_ByID_RejectsCrossTenant pins the read path's org predicate.
func TestSQLiteStore_ByID_RejectsCrossTenant(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	_, err := f.store.ByID(tenant.Into(t.Context(), f.orgB), victim.ID)
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)
}

// TestSQLiteStore_Reads_RejectMismatchedOrgArgument pins that an org argument disagreeing with the request scope matches nothing.
func TestSQLiteStore_Reads_RejectMismatchedOrgArgument(t *testing.T) {
	f := newHijackFixture(t)
	f.seedVictim(t)
	ctxB := tenant.Into(t.Context(), f.orgB)

	_, err := f.store.Current(ctxB, f.orgA.OrgID, f.orgA.ProjectID)
	assert.True(t, period.IsNotFoundError(err), "Current: got %T: %v", err, err)

	got, err := f.store.List(ctxB, f.orgA.OrgID, f.orgA.ProjectID, period.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, got, "List leaked another org's rows")

	_, err = f.store.Containing(ctxB, f.orgA.OrgID, f.orgA.ProjectID, civil.Date{Year: 2026, Month: 9, Day: 1})
	assert.True(t, period.IsNotFoundError(err), "Containing: got %T: %v", err, err)
}

// TestSQLiteStore_SaveClosing_RejectsCrossTenant pins that a closing cannot be appended into another org.
func TestSQLiteStore_SaveClosing_RejectsCrossTenant(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	closing := &period.Closing{
		ID:        uuid.Must(uuid.NewV7()),
		OrgID:     f.orgA.OrgID,
		ProjectID: f.orgA.ProjectID,
		PeriodID:  victim.ID,
		ClosedAt:  time.Now().UTC(),
		ClosedBy:  f.orgB.UserID,
		Snapshot:  sampleSnapshot(),
	}
	requireBlocked(t, f.store.SaveClosing(tenant.Into(t.Context(), f.orgB), closing), "cross-tenant SaveClosing")

	got, err := f.store.ListClosings(tenant.Into(t.Context(), f.orgA), f.orgA.OrgID, f.orgA.ProjectID, victim.ID)
	require.NoError(t, err)
	assert.Empty(t, got)
}
