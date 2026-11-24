//go:build integration

package user_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/schema"
)

func TestPostgres_StampSignIn(t *testing.T) {
	assertStampSignIn(t, newPostgresStoreForTest(t))
}

func TestAdminListOrgs_ReturnsCountsNotLedgerData(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	store := user.NewStore(db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}, db.Pool{W: sqlDB, R: sqlDB})

	assertAdminLists(t, store, sqlDB, h.Schema+"."+cfg.DB.TablePrefix)
}

func TestPostgres_SetSuspended(t *testing.T) {
	assertSetSuspended(t, newPostgresStoreForTest(t))
}
