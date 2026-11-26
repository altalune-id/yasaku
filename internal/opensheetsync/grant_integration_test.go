//go:build integration

package opensheetsync_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/altalune-id/oidcrp"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/nanoid"
	"altalune.id/yasaku/schema"
)

func newPgGrantFixture(t *testing.T) grantFixture {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	q := func(table string) string { return h.Schema + "." + cfg.DB.TablePrefix + table }
	tc, _ := seedPgTenant(t, sqlDB, q)
	other, _ := seedPgTenant(t, sqlDB, q)
	return grantFixture{
		grants: opensheetsync.NewGrantStore(db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix},
			db.Pool{W: sqlDB, R: sqlDB}, tenant.NewPgConn(sqlDB)),
		tc: tc, other: other, sealer: newSealer(t), rp: &fakes.OpensheetRefresher{},
	}
}

// NOTE: two Credentials stand in for two processes, so only the row lock keeps the refresh single.
func TestGrantSource_SingleFlightRefresh_Postgres(t *testing.T) {
	runSingleFlightRefresh(t, newPgGrantFixture(t), 2)
}

func TestGrantSource_RotatedTokenSavedOnError_Postgres(t *testing.T) {
	runRotatedTokenSavedOnError(t, newPgGrantFixture(t))
}

func TestGrantSource_InvalidGrantMarksReconnect_Postgres(t *testing.T) {
	runInvalidGrantMarksReconnect(t, newPgGrantFixture(t))
}

func TestGrantSource_RefreshNeverRecreatesADeletedGrant_Postgres(t *testing.T) {
	runRefreshNeverRecreatesADeletedGrant(t, newPgGrantFixture(t))
}

// NOTE: the app role is NOBYPASSRLS, as in production, so the forced policy on opensheet_grants is under test, not only the store's org predicate.
func TestGrantStore_RLSIsolatesOrgs_Postgres(t *testing.T) {
	h := pgtest.New(t)
	s, err := nanoid.New(10)
	require.NoError(t, err)
	suffix := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(s))
	ownerRole, appRole, pfx := "yasaku_grantowner_"+suffix, "yasaku_grantapp_"+suffix, "g"+suffix+"_"
	admin, err := db.Open(t.Context(), db.DBConfig{Driver: db.DriverPostgres, DSN: h.DSN}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	run := func(stmt string) {
		t.Helper()
		_, err := admin.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}
	for _, r := range []struct{ name, attrs string }{{ownerRole, "NOLOGIN BYPASSRLS"}, {appRole, "LOGIN PASSWORD 'pw' NOBYPASSRLS"}} {
		run(fmt.Sprintf(`CREATE ROLE %q %s`, r.name, r.attrs))
		t.Cleanup(func() {
			_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP OWNED BY %q`, r.name))
			_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP ROLE IF EXISTS %q`, r.name))
		})
		run(fmt.Sprintf(`GRANT %q TO CURRENT_USER`, r.name))
	}
	run(fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA public TO %q`, ownerRole))
	run(fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %q`, appRole))
	run(fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE %q IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %q`, ownerRole, appRole))
	run(fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE %q IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO %q`, ownerRole, appRole))

	migDB, err := db.Open(t.Context(), db.DBConfig{Driver: db.DriverPostgres, DSN: h.DSN, Role: ownerRole, MaxOpenConns: 1}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migDB.Close() })
	cfg := config.Defaults()
	cfg.DB.Driver, cfg.DB.Schema, cfg.DB.TablePrefix, cfg.Tenant.RLSEnforce = db.DriverPostgres, "public", pfx, true
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))
	q := func(table string) string { return "public." + pfx + table }
	orgA, _ := seedPgTenant(t, migDB, q)
	orgB, _ := seedPgTenant(t, migDB, q)

	appConn, err := db.Open(t.Context(), db.DBConfig{Driver: db.DriverPostgres, DSN: pgtest.DSNWithUser(t, h.DSN, appRole, "pw"), MaxOpenConns: 2}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = appConn.Close() })
	pc := tenant.NewPgConn(appConn)
	grants := opensheetsync.NewGrantStore(db.DBConfig{Driver: db.DriverPostgres, Schema: "public", TablePrefix: pfx}, db.Pool{W: appConn, R: appConn}, pc)
	sl := newSealer(t)
	ctxA, ctxB := fakes.Owner(context.Background(), orgA), fakes.Owner(context.Background(), orgB)
	grantB, err := opensheetsync.NewGrant(sl, orgB.OrgID, orgB.ProjectID, orgB.UserID, oidcrp.TokenSet{RefreshToken: "b-refresh"}, time.Now())
	require.NoError(t, err)
	require.NoError(t, grants.SaveGrant(ctxB, grantB))

	_, err = grants.GrantByProject(ctxA, orgB.OrgID, orgB.ProjectID)
	require.True(t, opensheetsync.IsGrantNotFoundError(err), "org A reads no grant of org B's: %v", err)
	listed, err := grants.GrantsConnectedBy(ctxA, orgB.OrgID, orgB.UserID)
	require.NoError(t, err)
	require.Empty(t, listed, "org A lists none of org B's grants")
	require.True(t, opensheetsync.IsGrantNotFoundError(grants.DeleteGrant(ctxA, orgB.OrgID, orgB.ProjectID)), "org A deletes nothing of org B's")
	require.Error(t, grants.SaveGrant(ctxA, grantB), "org A cannot write a grant row for org B")
	require.True(t, opensheetsync.IsGrantNotFoundError(grants.UpdateGrantTokens(ctxA, grantB)), "org A updates nothing of org B's")

	var visible int
	require.NoError(t, tenant.RunInTx(t.Context(), pc, orgA, func(ctx context.Context) error {
		tx, _ := db.CurrentTx(ctx)
		return tx.QueryRowContext(ctx, "SELECT count(*) FROM "+q("opensheet_grants")).Scan(&visible)
	}))
	require.Zero(t, visible, "without any predicate, the policy alone hides org B's grant from org A")

	got, err := grants.GrantByProject(ctxB, orgB.OrgID, orgB.ProjectID)
	require.NoError(t, err)
	rt, err := got.RefreshToken(sl)
	require.NoError(t, err)
	require.Equal(t, "b-refresh", rt, "org B's grant is untouched")
}
