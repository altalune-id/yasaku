package period_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

func TestSQLiteStore_Save_RejectsSiblingProject(t *testing.T) {
	sqlDB, cfg := newSQLiteDB(t)
	prefix := cfg.DB.TablePrefix
	tc := seedTenant(t, sqlDB, prefix)
	store := period.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix}, db.Pool{W: sqlDB, R: sqlDB}, nil)

	victim, err := period.New(tc.OrgID, tc.ProjectID, civil.Date{Year: 2026, Month: 8, Day: 25}, "project A's period")
	require.NoError(t, err)
	require.NoError(t, store.Save(tenant.Into(t.Context(), tc), victim))

	projID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err = sqlDB.Exec(
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Sibling', ?, ?, ?)",
		projID.String(), tc.OrgID.String(), projID.String()[:8], tc.UserID.String(), now, now)
	require.NoError(t, err)
	sibling := tenant.Context{OrgID: tc.OrgID, ProjectID: projID, UserID: tc.UserID}

	attack := *victim
	attack.Name = "Hijacked"
	requireBlocked(t, store.Save(tenant.Into(t.Context(), sibling), &attack), "sibling-project Save")

	got, err := store.ByID(tenant.Into(t.Context(), tc), victim.ID)
	require.NoError(t, err)
	require.Equal(t, "project A's period", got.Name)
}
