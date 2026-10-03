package transaction_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

func (f txnHijackFixture) siblingProject(t *testing.T) tenant.Context {
	t.Helper()
	projID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	if _, err := f.db.Exec(
		"INSERT INTO "+f.prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Sibling', ?, ?, ?)",
		projID.String(), f.orgA.OrgID.String(), projID.String()[:8], f.orgA.UserID.String(), now, now); err != nil {
		t.Fatalf("seed sibling project: %v", err)
	}
	return tenant.Context{OrgID: f.orgA.OrgID, ProjectID: projID, UserID: f.orgA.UserID}
}

func TestSQLiteStore_Save_RejectsSiblingProject(t *testing.T) {
	f := newTxnHijackFixture(t)
	victim := f.seedVictim(t)
	sibling := f.siblingProject(t)

	attack := *victim
	attack.Note = "Hijacked"
	requireTxnBlocked(t, f.store.Save(tenant.Into(context.Background(), sibling), &attack), "sibling-project Save")
	f.requireNote(t, victim.ID, "org A's transaction")
}

func TestSQLiteStore_Delete_RejectsSiblingProject(t *testing.T) {
	f := newTxnHijackFixture(t)
	victim := f.seedVictim(t)
	sibling := f.siblingProject(t)

	requireTxnBlocked(t, f.store.Delete(tenant.Into(context.Background(), sibling), victim.ID), "sibling-project Delete")
	f.requireNote(t, victim.ID, "org A's transaction")
}
