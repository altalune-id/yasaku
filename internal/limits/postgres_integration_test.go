//go:build integration

package limits_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestPostgresStore_ResolveAndLock(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	dbCfg := db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}
	pool := db.Pool{W: sqlDB, R: sqlDB}
	store := limits.NewStore(dbCfg, pool)
	prefix := cfg.DB.TablePrefix

	ownerID, orgID := uuid.New(), uuid.New()
	_, err := sqlDB.ExecContext(t.Context(), "INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, now(), now())", ownerID, ownerID.String()+"@x.co")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(), "INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at, system, plan) VALUES ($1, 'acme', 'Acme', $2, now(), now(), false, 'pro')", orgID, ownerID)
	require.NoError(t, err)

	sub := limits.Subject{Kind: limits.SubjectOrg, ID: orgID}
	ctx := tenant.Into(t.Context(), tenant.Context{OrgID: orgID, UserID: ownerID})
	require.NoError(t, store.SetPlan(ctx, "pro", limits.OrgProjects, 8, ownerID))
	require.NoError(t, store.SetPlan(ctx, "pro", limits.OrgProjects, 7, uuid.Nil))
	v, ok, err := store.Resolve(ctx, limits.OrgProjects, sub)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(7), v)

	require.NoError(t, store.SetOverride(ctx, sub, limits.OrgProjects, limits.Unlimited, ownerID))
	v, _, err = store.Resolve(ctx, limits.OrgProjects, sub)
	require.NoError(t, err)
	assert.Equal(t, limits.Unlimited, v)
	overrides, err := store.ListOverrides(ctx)
	require.NoError(t, err)
	assert.Equal(t, []limits.Override{{Subject: sub, Key: limits.OrgProjects, Value: limits.Unlimited}}, overrides)
	require.NoError(t, store.DeleteOverride(ctx, sub, limits.OrgProjects))

	svc := limits.NewService(store, config.ModeCloud)
	uow := tenant.NewUnitOfWork(dbCfg, pool, tenant.NewPgConn(sqlDB))
	require.NoError(t, uow(ctx, func(ctx context.Context) error {
		return svc.Lock(ctx, limits.Subject{Kind: limits.SubjectUser, ID: ownerID}, sub)
	}))
	err = uow(ctx, func(ctx context.Context) error {
		return svc.Lock(ctx, limits.Subject{Kind: limits.SubjectProject, ID: uuid.New()})
	})
	assert.True(t, limits.IsSubjectMissingError(err), "a lock that holds no row must fail closed, got %T: %v", err, err)
}
