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

func TestMigrateUp_SQLite_OpensheetTables(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "m.db") + "?_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	cfg := config.Defaults()
	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	exec := func(q string, args ...any) error {
		_, err := db.ExecContext(ctx, q, args...)
		return err
	}
	must := func(q string, args ...any) {
		t.Helper()
		if err := exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	const ts = "2026-01-01T00:00:00.000000000Z"
	must(`INSERT INTO yasaku_users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ('u1', 'u1@x.co', '', '', 0, ?, ?)`, ts, ts)
	must(`INSERT INTO yasaku_orgs (id, slug, name, created_by, created_at, updated_at) VALUES ('o1', 'o1', 'O', 'u1', ?, ?)`, ts, ts)
	must(`INSERT INTO yasaku_projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ('p1', 'o1', 'p1', 'P', 'u1', ?, ?)`, ts, ts)
	link := `INSERT INTO yasaku_opensheet_links (id, org_id, project_id, os_org, os_project, api_key_sealed, api_key_hint,
		transactions_sheet, wallets_sheet, categories_sheet, enabled, created_by, created_by_key_id, created_at, updated_at)
		VALUES (?, 'o1', 'p1', 'acme', 'home', x'01', 'abcd', ?, ?, ?, 0, ?, NULL, ?, ?)`
	if err := exec(link, "l0", "a", "a", "c", "u1", ts, ts); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("two tabs on one sheet slug must be refused by a CHECK, got %v", err)
	}
	if err := exec(link, "l0", "a", "b", "c", nil, ts, ts); err == nil {
		t.Fatal("a link with no author must be refused")
	}
	must(link, "l1", "a", "b", "c", "u1", ts, ts)
	if err := exec(link, "l2", "a", "b", "c", "u1", ts, ts); err == nil {
		t.Fatal("a second link for one project must be refused")
	}
	must(`INSERT INTO yasaku_orgs (id, slug, name, created_by, created_at, updated_at) VALUES ('o2', 'o2', 'O2', 'u1', ?, ?)`, ts, ts)
	if err := exec(strings.Replace(link, "'o1'", "'o2'", 1), "l3", "a", "b", "c", "u1", ts, ts); err == nil {
		t.Fatal("a second link for one project must be refused even under another org")
	}
	state := `INSERT INTO yasaku_opensheet_sync_state (org_id, project_id, entity, entity_id, version, updated_at) VALUES ('o1', ?, ?, ?, 1, ?)`
	must(state, "p1", "wallet", "w1", ts)
	for _, col := range []string{"synced_version", "attempts"} {
		q := `INSERT INTO yasaku_opensheet_sync_state (org_id, project_id, entity, entity_id, version, ` + col + `, updated_at) VALUES ('o1', 'p1', 'wallet', ?, 1, -1, ?)`
		if err := exec(q, "neg-"+col, ts); err == nil || !strings.Contains(err.Error(), "CHECK") {
			t.Fatalf("a negative %s must be refused by a CHECK, got %v", col, err)
		}
	}
	if ddl, ok := sqliteIndexes(ctx, t, db, "yasaku_opensheet_sync_state")["yasaku_opensheet_sync_state_link_idx"]; !ok || !strings.Contains(ddl, "(org_id, project_id)") {
		t.Fatalf("the link FK needs an (org_id, project_id) index so a link delete cascade does not scan, got %q", ddl)
	}
	if err := exec(state, "p1", "period", "x1", ts); err == nil {
		t.Fatal("an unknown entity must be refused")
	}
	if err := exec(state, "p-missing", "wallet", "w2", ts); err == nil {
		t.Fatal("state for a project without a link must be refused by the composite FK")
	}
	must(`DELETE FROM yasaku_opensheet_links WHERE id = 'l1'`)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM yasaku_opensheet_sync_state`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("removing the link must cascade to its state rows, %d left", n)
	}
	if err := MigrateDownTo(ctx, db, cfg, 13); err != nil {
		t.Fatalf("MigrateDownTo 13: %v", err)
	}
	if err := exec(`SELECT 1 FROM yasaku_opensheet_links`); err == nil {
		t.Fatal("down must drop opensheet_links")
	}
}
