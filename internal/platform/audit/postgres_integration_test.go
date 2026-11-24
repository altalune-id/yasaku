//go:build integration

package audit_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/nanoid"
	"altalune.id/yasaku/schema"
)

// TestAuditEvents_AppendOnly runs as a NOBYPASSRLS app role. SECURITY: only SELECT and INSERT policies exist, so an UPDATE or DELETE touches no row, and a row for another org cannot be inserted.
func TestAuditEvents_AppendOnly(t *testing.T) {
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	ownerRole, appRole, pfx := "yasaku_auditowner_"+suffix, "yasaku_auditapp_"+suffix, "a"+suffix+"_"

	admin, err := db.Open(t.Context(), db.DBConfig{Driver: db.DriverPostgres, DSN: h.DSN}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	createRole(t, admin, ownerRole, "NOLOGIN BYPASSRLS")
	exec(t, admin, fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA public TO %q`, ownerRole))
	createRole(t, admin, appRole, "LOGIN PASSWORD 'pw' NOBYPASSRLS")
	exec(t, admin, fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %q`, appRole))
	exec(t, admin, fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE %q IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %q`, ownerRole, appRole))
	exec(t, admin, fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE %q IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO %q`, ownerRole, appRole))

	migDB, err := db.Open(t.Context(), db.DBConfig{Driver: db.DriverPostgres, DSN: h.DSN, Role: ownerRole, MaxOpenConns: 1}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migDB.Close() })
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.Schema = "public"
	cfg.DB.TablePrefix = pfx
	cfg.Tenant.RLSEnforce = true
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))

	appConn, err := db.Open(t.Context(), db.DBConfig{Driver: db.DriverPostgres, DSN: pgtest.DSNWithUser(t, h.DSN, appRole, "pw"), MaxOpenConns: 2}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = appConn.Close() })
	dbCfg := db.DBConfig{Driver: db.DriverPostgres, Schema: "public", TablePrefix: pfx}
	pool := db.Pool{W: appConn, R: appConn}
	pc := tenant.NewPgConn(appConn)
	rec := audit.NewRecorder(dbCfg, pool, pc)
	uow := tenant.NewUnitOfWork(dbCfg, pool, pc)

	orgID := uuid.New()
	ctx := scoped(orgID)
	require.NoError(t, uow(ctx, func(ctx context.Context) error {
		return rec.Record(ctx, audit.Event{Action: "org.renamed", Detail: map[string]string{"to_name": "Acme"}})
	}))
	entries, err := rec.List(ctx, audit.Filter{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "Acme", entries[0].Detail["to_name"])

	tc := tenant.Context{OrgID: orgID}
	require.NoError(t, tenant.RunInTx(t.Context(), pc, tc, func(ctx context.Context) error {
		tx, _ := db.CurrentTx(ctx)
		for _, stmt := range []string{"UPDATE " + pfx + "audit_events SET action = 'tampered'", "DELETE FROM " + pfx + "audit_events"} {
			res, err := tx.ExecContext(ctx, stmt)
			require.NoError(t, err)
			n, _ := res.RowsAffected()
			require.Zero(t, n, "%s must touch no row", stmt)
		}
		return nil
	}))
	entries, err = rec.List(ctx, audit.Filter{})
	require.NoError(t, err)
	require.Equal(t, "org.renamed", entries[0].Action, "the row survives untouched")

	err = uow(ctx, func(ctx context.Context) error {
		return rec.Record(tenant.Into(ctx, tenant.Context{OrgID: uuid.New()}), audit.Event{Action: "forged"})
	})
	require.Error(t, err, "a row for another org fails the INSERT policy")
}

// SECURITY: with row level security off the policies are gone, so the trigger alone keeps the log append-only.
func TestAuditEvents_AppendOnlyWithoutRLS(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	cfg.Tenant.RLSEnforce = false
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	table := h.Schema + "." + cfg.DB.TablePrefix + "audit_events"
	exec(t, sqlDB, "INSERT INTO "+table+" (id, org_id, actor_kind, action, occurred_at) VALUES (gen_random_uuid(), gen_random_uuid(), 'system', 'org.renamed', now())")

	for _, stmt := range []string{"UPDATE " + table + " SET action = 'tampered'", "DELETE FROM " + table} {
		_, err := sqlDB.ExecContext(t.Context(), stmt)
		require.Error(t, err, "%s must be refused", stmt)
	}
	var action string
	require.NoError(t, sqlDB.QueryRowContext(t.Context(), "SELECT action FROM "+table).Scan(&action))
	require.Equal(t, "org.renamed", action)
}

func exec(t *testing.T, conn *sql.DB, stmt string) {
	t.Helper()
	_, err := conn.ExecContext(t.Context(), stmt)
	require.NoError(t, err)
}

func uniqueSuffix(t *testing.T) string {
	t.Helper()
	s, err := nanoid.New(10)
	require.NoError(t, err)
	return strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(s))
}

func createRole(t *testing.T, admin *sql.DB, name, attrs string) {
	t.Helper()
	exec(t, admin, fmt.Sprintf(`CREATE ROLE %q %s`, name, attrs))
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP OWNED BY %q`, name))
		_, _ = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP ROLE IF EXISTS %q`, name))
	})
	exec(t, admin, fmt.Sprintf(`GRANT %q TO CURRENT_USER`, name))
}
