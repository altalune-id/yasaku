package category_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

func siblingProject(t *testing.T, sqlDB *sql.DB, prefix string, tc tenant.Context) tenant.Context {
	t.Helper()
	projID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Sibling', ?, ?, ?)",
		projID.String(), tc.OrgID.String(), projID.String()[:8], tc.UserID.String(), now, now)
	require.NoError(t, err)
	return tenant.Context{OrgID: tc.OrgID, ProjectID: projID, UserID: tc.UserID}
}

func seedProjectVictim(t *testing.T, store category.Store, tc tenant.Context) *category.Category {
	t.Helper()
	victim, err := category.New(tc.OrgID, tc.ProjectID, "project A's category", category.KindExpense, "", "", 0)
	require.NoError(t, err)
	require.NoError(t, store.Save(tenant.Into(t.Context(), tc), victim))
	return victim
}

func TestSQLite_Save_RejectsSiblingProject(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteFixture(t)
	victim := seedProjectVictim(t, store, tc)
	sibling := siblingProject(t, sqlDB, prefix, tc)

	attack := *victim
	attack.Name = "Hijacked"
	err := store.Save(tenant.Into(t.Context(), sibling), &attack)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)

	got, err := store.ByID(tenant.Into(t.Context(), tc), victim.ID)
	require.NoError(t, err)
	require.Equal(t, "project A's category", got.Name)
}

func TestSQLite_Delete_RejectsSiblingProject(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteFixture(t)
	victim := seedProjectVictim(t, store, tc)
	sibling := siblingProject(t, sqlDB, prefix, tc)

	err := store.Delete(tenant.Into(t.Context(), sibling), victim.ID)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)

	_, err = store.ByID(tenant.Into(t.Context(), tc), victim.ID)
	require.NoError(t, err)
}
