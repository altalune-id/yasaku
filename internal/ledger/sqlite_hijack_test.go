package ledger_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

type hijackFixture struct {
	store ledger.Store
	orgA  tenant.Context
	orgB  tenant.Context
}

func newHijackFixture(t *testing.T) hijackFixture {
	t.Helper()
	sqlDB, cfg := newSQLiteDB(t)

	uidA, oidA, pidA := seedTenant(t, sqlDB, cfg.DB.TablePrefix)
	uidB, oidB, pidB := seedTenant(t, sqlDB, cfg.DB.TablePrefix)

	store := ledger.NewStore(
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

func (f hijackFixture) seedVictim(t *testing.T) *ledger.Settings {
	t.Helper()
	victim := ledger.Defaults(f.orgA.OrgID, f.orgA.ProjectID)
	if err := victim.Apply(ledger.Patch{Timezone: ptr("Asia/Jakarta"), PeriodStartDay: ptr(3)}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := f.store.Save(tenant.Into(context.Background(), f.orgA), victim); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	return victim
}

func (f hijackFixture) requireTimezone(t *testing.T, projectID uuid.UUID, want string) {
	t.Helper()
	got, err := f.store.ByProject(tenant.Into(context.Background(), f.orgA), f.orgA.OrgID, projectID)
	if err != nil {
		t.Fatalf("ByProject: %v", err)
	}
	if got.Timezone != want {
		t.Fatalf("org A's row reads %q, want %q: another org rewrote it across the tenant boundary", got.Timezone, want)
	}
}

func requireBlocked(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: succeeded, want a cross-tenant write to be refused", what)
	}
	if !ledger.IsNotFoundError(err) {
		t.Fatalf("%s: got %T %v, want *ledger.NotFoundError", what, err, err)
	}
}

// TestSQLiteStore_Save_RejectsReskinnedHijack covers the handler-shaped attack: the attacker's own org carrying the victim's project id.
func TestSQLiteStore_Save_RejectsReskinnedHijack(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	attack := ledger.Defaults(f.orgB.OrgID, victim.ProjectID)
	if err := attack.Apply(ledger.Patch{Timezone: ptr("Europe/Berlin")}); err != nil {
		t.Fatal(err)
	}

	requireBlocked(t, f.store.Save(tenant.Into(context.Background(), f.orgB), attack), "reskinned Save")
	f.requireTimezone(t, victim.ProjectID, "Asia/Jakarta")
}

// TestSQLiteStore_Save_RejectsVerbatimHijack covers the copied-row shape: the victim's org and project id replayed under the attacker's scope.
func TestSQLiteStore_Save_RejectsVerbatimHijack(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	attack := *victim
	attack.Timezone = "Europe/Berlin"

	requireBlocked(t, f.store.Save(tenant.Into(context.Background(), f.orgB), &attack), "verbatim Save")
	f.requireTimezone(t, victim.ProjectID, "Asia/Jakarta")
}

// TestSQLiteStore_Save_UpdatesOwnRow pins that the tenant predicate still lets the owning org through — a WHERE(false) guard would pass every hijack test without it.
func TestSQLiteStore_Save_UpdatesOwnRow(t *testing.T) {
	f := newHijackFixture(t)
	victim := f.seedVictim(t)

	if err := victim.Apply(ledger.Patch{Timezone: ptr("Europe/Berlin")}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Save(tenant.Into(context.Background(), f.orgA), victim); err != nil {
		t.Fatalf("owner Save: %v", err)
	}
	f.requireTimezone(t, victim.ProjectID, "Europe/Berlin")
}
