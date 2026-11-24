package schema

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
)

const sqliteRolesTS = "2026-01-01T00:00:00Z"

func openSQLiteRoles(t *testing.T) (*sql.DB, *config.Config) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "r.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := config.Defaults()
	if err := MigrateUp(t.Context(), db, cfg); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	sqliteRolesExec(t, db, `INSERT INTO yasaku_users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ('u1', 'u1@x.co', '', '', 0, ?, ?)`, sqliteRolesTS, sqliteRolesTS)
	return db, cfg
}

func sqliteRolesExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func sqliteRolesRefused(t *testing.T, db *sql.DB, want, msg, q string, args ...any) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), q, args...)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("%s = %v, want %q", msg, err, want)
	}
}

func sqliteRolesOrg(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	sqliteRolesExec(t, db, `INSERT INTO yasaku_orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'O', 'u1', ?, ?)`, id, id, sqliteRolesTS, sqliteRolesTS)
}

func sqliteRolesRole(t *testing.T, db *sql.DB, org, id string) {
	t.Helper()
	sqliteRolesExec(t, db, `INSERT INTO yasaku_roles (id, org_id, name, permissions, created_at, updated_at) VALUES (?, ?, ?, '["wallets:read"]', ?, ?)`, id, org, id, sqliteRolesTS, sqliteRolesTS)
}

const (
	sqliteRolesMember = `INSERT INTO yasaku_memberships (id, org_id, user_id, role, role_id, created_at) VALUES (?, ?, 'u1', ?, ?, '` + sqliteRolesTS + `')`
	sqliteRolesInvite = `INSERT INTO yasaku_invites (id, org_id, email, role, role_id, token_hash, expires_at, invited_by, created_at) VALUES (?, ?, 'i@x.co', ?, ?, ?, '` + sqliteRolesTS + `', 'u1', '` + sqliteRolesTS + `')`
)

func TestMigration024_SQLite_ExistingRowsValid(t *testing.T) {
	db, cfg := openSQLiteRoles(t)
	if err := MigrateDownTo(t.Context(), db, cfg, 20); err != nil {
		t.Fatalf("MigrateDownTo: %v", err)
	}
	sqliteRolesOrg(t, db, "o1")
	sqliteRolesExec(t, db, `INSERT INTO yasaku_memberships (id, org_id, user_id, role, system, created_at) VALUES ('m1', 'o1', 'u1', 'owner', 1, ?)`, sqliteRolesTS)
	sqliteRolesExec(t, db, `INSERT INTO yasaku_invites (id, org_id, email, role, token_hash, expires_at, accepted_at, invited_by, created_at) VALUES ('i1', 'o1', 'i@x.co', 'admin', 'h1', ?, NULL, 'u1', ?)`, sqliteRolesTS, sqliteRolesTS)

	if err := MigrateUp(t.Context(), db, cfg); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	var role string
	var system int
	if err := db.QueryRowContext(t.Context(), `SELECT role, system FROM yasaku_memberships WHERE id = 'm1' AND role_id IS NULL`).Scan(&role, &system); err != nil {
		t.Fatalf("membership after rebuild: %v", err)
	}
	if role != "owner" || system != 1 {
		t.Errorf("membership = %s/%d, want owner/1", role, system)
	}
	var hash string
	if err := db.QueryRowContext(t.Context(), `SELECT token_hash FROM yasaku_invites WHERE id = 'i1' AND role = 'admin' AND role_id IS NULL`).Scan(&hash); err != nil || hash != "h1" {
		t.Errorf("invite after rebuild = %q, %v", hash, err)
	}
	var fkErrors int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&fkErrors); err != nil || fkErrors != 0 {
		t.Errorf("foreign_key_check after rebuild = %d, %v", fkErrors, err)
	}
	for _, idx := range []string{"yasaku_memberships_user_idx", "yasaku_invites_email_pending_idx"} {
		var n int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idx).Scan(&n); err != nil || n != 1 {
			t.Errorf("index %s after rebuild: %d, %v", idx, n, err)
		}
	}
}

func TestMigration024_SQLite_CustomNeedsRoleID(t *testing.T) {
	db, _ := openSQLiteRoles(t)
	sqliteRolesOrg(t, db, "o1")
	sqliteRolesRole(t, db, "o1", "r1")

	sqliteRolesRefused(t, db, "CHECK constraint failed", "custom membership with no role_id", sqliteRolesMember, "m1", "o1", "custom", nil)
	sqliteRolesRefused(t, db, "CHECK constraint failed", "built-in membership with a role_id", sqliteRolesMember, "m2", "o1", "member", "r1")
	sqliteRolesRefused(t, db, "CHECK constraint failed", "unknown role", sqliteRolesMember, "m3", "o1", "viewer", nil)
	sqliteRolesRefused(t, db, "CHECK constraint failed", "custom invite with no role_id", sqliteRolesInvite, "i1", "o1", "custom", nil, "h1")
	sqliteRolesRefused(t, db, "CHECK constraint failed", "built-in invite with a role_id", sqliteRolesInvite, "i2", "o1", "admin", "r1", "h2")
	sqliteRolesExec(t, db, sqliteRolesMember, "m4", "o1", "custom", "r1")
	sqliteRolesExec(t, db, sqliteRolesInvite, "i3", "o1", "custom", "r1", "h3")
	sqliteRolesRefused(t, db, "UNIQUE constraint failed", "role names are unique per org, ignoring case",
		`INSERT INTO yasaku_roles (id, org_id, name, permissions, created_at, updated_at) VALUES ('r2', 'o1', 'R1', '[]', ?, ?)`, sqliteRolesTS, sqliteRolesTS)
}

func TestMigration024_SQLite_CrossOrgRoleRefused(t *testing.T) {
	db, _ := openSQLiteRoles(t)
	sqliteRolesOrg(t, db, "oa")
	sqliteRolesOrg(t, db, "ob")
	sqliteRolesRole(t, db, "oa", "ra")

	sqliteRolesRefused(t, db, "FOREIGN KEY constraint failed", "membership naming another org's role", sqliteRolesMember, "m1", "ob", "custom", "ra")
	sqliteRolesRefused(t, db, "FOREIGN KEY constraint failed", "invite naming another org's role", sqliteRolesInvite, "i1", "ob", "custom", "ra", "h1")

	sqliteRolesExec(t, db, sqliteRolesMember, "m2", "oa", "custom", "ra")
	sqliteRolesRefused(t, db, "FOREIGN KEY constraint failed", "deleting a held role", `DELETE FROM yasaku_roles WHERE id = 'ra'`)
	sqliteRolesExec(t, db, `DELETE FROM yasaku_orgs WHERE id = 'oa'`)
	var left int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM yasaku_roles WHERE org_id = 'oa'`).Scan(&left); err != nil || left != 0 {
		t.Errorf("roles after org delete = %d, %v", left, err)
	}
}
