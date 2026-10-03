//go:build integration

package period_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestPostgres_Period_CloseKeyAuthor(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	prefix := cfg.DB.TablePrefix
	tc := seedPgTenant(t, sqlDB, prefix)
	keyID := uuid.New()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"api_keys (id, org_id, project_id, name, secret_hash, created_at) VALUES ($1, $2, $3, 'ci', '\\x01', $4)",
		keyID, tc.OrgID, tc.ProjectID, time.Now().UTC())
	require.NoError(t, err)

	pc := tenant.NewPgConn(sqlDB)
	store := period.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB}, pc)
	svc := newAuthorCloseService(t, store, func(ctx context.Context, fn func(ctx context.Context) error) error {
		scope, tErr := tenant.From(ctx)
		if tErr != nil {
			return tErr
		}
		return tenant.RunInTx(ctx, pc, scope, fn)
	})
	checkCloseKeyAuthor(t, sqlDB, store, svc, tc, keyID,
		"SELECT closed_by::text, closed_by_key_id::text FROM "+prefix+"period_closings WHERE period_id = $1")
}

func TestPostgres_Period_CloseKeyAuthorLegacyNilAuthor(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	prefix := cfg.DB.TablePrefix
	tc := seedPgTenant(t, sqlDB, prefix)
	pc := tenant.NewPgConn(sqlDB)
	store := period.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB}, pc)
	svc := newAuthorCloseService(t, store, func(ctx context.Context, fn func(ctx context.Context) error) error {
		scope, tErr := tenant.From(ctx)
		if tErr != nil {
			return tErr
		}
		return tenant.RunInTx(ctx, pc, scope, fn)
	})
	checkLegacyNilAuthorClosing(t, sqlDB, store, svc, tc, func(periodID uuid.UUID) {
		_, err := sqlDB.ExecContext(t.Context(),
			"INSERT INTO "+prefix+"period_closings (id, org_id, project_id, period_id, closed_at, closed_by, snapshot) VALUES ($1, $2, $3, $4, $5, $6, '{}')",
			uuid.New(), tc.OrgID, tc.ProjectID, periodID, time.Date(2026, 7, 25, 3, 0, 0, 0, time.UTC), uuid.Nil)
		require.NoError(t, err)
	})
}
