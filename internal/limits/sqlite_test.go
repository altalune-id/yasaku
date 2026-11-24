package limits_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/schema"
)

func newSQLiteLimits(t *testing.T) (limits.Store, *sql.DB, string) {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	cfg := config.Defaults()
	require.NoError(t, schema.MigrateUp(context.Background(), sqlDB, cfg))
	return limits.NewStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix}, db.Pool{W: sqlDB, R: sqlDB}), sqlDB, cfg.DB.TablePrefix
}

func TestSQLiteStore_ResolveOrder(t *testing.T) {
	store, sqlDB, prefix := newSQLiteLimits(t)
	ctx := context.Background()
	orgID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec("INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at, system, plan) VALUES (?, 'acme', 'Acme', ?, ?, ?, 0, 'pro')",
		orgID.String(), uuid.NewString(), now, now)
	require.NoError(t, err)
	sub := limits.Subject{Kind: limits.SubjectOrg, ID: orgID}

	_, ok, err := store.Resolve(ctx, limits.OrgProjects, sub)
	require.NoError(t, err)
	assert.False(t, ok, "no rows falls back to the catalogue")

	require.NoError(t, store.SetPlan(ctx, limits.DefaultPlan, limits.OrgProjects, 9, uuid.Nil))
	_, ok, err = store.Resolve(ctx, limits.OrgProjects, sub)
	require.NoError(t, err)
	assert.False(t, ok, "another plan's row does not apply to an org on pro")

	require.NoError(t, store.SetPlan(ctx, "pro", limits.OrgProjects, 7, uuid.New()))
	v, ok, err := store.Resolve(ctx, limits.OrgProjects, sub)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(7), v)

	require.NoError(t, store.SetOverride(ctx, sub, limits.OrgProjects, limits.Unlimited, uuid.Nil))
	v, _, err = store.Resolve(ctx, limits.OrgProjects, sub)
	require.NoError(t, err)
	assert.Equal(t, limits.Unlimited, v, "a NULL override is unlimited")

	require.NoError(t, store.DeleteOverride(ctx, sub, limits.OrgProjects))
	v, _, err = store.Resolve(ctx, limits.OrgProjects, sub)
	require.NoError(t, err)
	assert.Equal(t, int64(7), v, "deleting the override reverts to the plan row")

	user := limits.Subject{Kind: limits.SubjectUser, ID: uuid.New()}
	require.NoError(t, store.SetPlan(ctx, limits.DefaultPlan, limits.UserOrgsJoined, 2, uuid.Nil))
	v, ok, err = store.Resolve(ctx, limits.UserOrgsJoined, user)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(2), v, "a user subject resolves against the default plan")

	plan, err := store.ListPlan(ctx, "pro")
	require.NoError(t, err)
	assert.Equal(t, []limits.PlanEntry{{Plan: "pro", Key: limits.OrgProjects, Value: 7}}, plan)
	require.NoError(t, store.ClearPlan(ctx, "pro", limits.OrgProjects))
	_, ok, err = store.Resolve(ctx, limits.OrgProjects, sub)
	require.NoError(t, err)
	assert.False(t, ok)
}
