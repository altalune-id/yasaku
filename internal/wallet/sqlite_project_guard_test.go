package wallet_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

func (f sqliteFixture) siblingProject(t *testing.T) tenant.Context {
	t.Helper()
	projID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := f.db.Exec(
		"INSERT INTO "+f.prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Sibling', ?, ?, ?)",
		projID.String(), f.tc.OrgID.String(), projID.String()[:8], f.tc.UserID.String(), now, now)
	require.NoError(t, err)
	return tenant.Context{OrgID: f.tc.OrgID, ProjectID: projID, UserID: f.tc.UserID}
}

func TestSQLiteStore_Save_RejectsSiblingProject(t *testing.T) {
	f := newSQLiteFixture(t)
	victim := newSQLiteWallet(t, f, "project A's wallet")
	sibling := f.siblingProject(t)

	attack := *victim
	attack.Name = "Hijacked"
	requireBlocked(t, f.store.Save(tenant.Into(t.Context(), sibling), &attack), "sibling-project Save")

	got, err := f.store.ByID(f.ctx(t), victim.ID)
	require.NoError(t, err)
	require.Equal(t, "project A's wallet", got.Name)
}

func TestSQLiteStore_Delete_RejectsSiblingProject(t *testing.T) {
	f := newSQLiteFixture(t)
	victim := newSQLiteWallet(t, f, "project A's wallet")
	sibling := f.siblingProject(t)

	requireBlocked(t, f.store.Delete(tenant.Into(t.Context(), sibling), victim.ID), "sibling-project Delete")

	_, err := f.store.ByID(f.ctx(t), victim.ID)
	require.NoError(t, err)
}
