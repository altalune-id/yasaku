package schema

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
)

const sqliteGrantsTS = "2026-01-01T00:00:00Z"

func TestMigration022_SyncStateSurvivesRebuild(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "g.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	cfg := config.Defaults()
	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	if err := MigrateDownTo(ctx, db, cfg, 21); err != nil {
		t.Fatalf("MigrateDownTo 21: %v", err)
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
	must(`INSERT INTO yasaku_users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ('u1', 'u1@x.co', '', '', 0, ?, ?)`, sqliteGrantsTS, sqliteGrantsTS)
	must(`INSERT INTO yasaku_orgs (id, slug, name, created_by, created_at, updated_at) VALUES ('o1', 'o1', 'O', 'u1', ?, ?)`, sqliteGrantsTS, sqliteGrantsTS)
	for _, p := range []string{"p1", "p2", "p3"} {
		must(`INSERT INTO yasaku_projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, 'o1', ?, 'P', 'u1', ?, ?)`, p, p, sqliteGrantsTS, sqliteGrantsTS)
	}
	must(`INSERT INTO yasaku_opensheet_links (id, org_id, project_id, os_org, os_project, api_key_sealed, api_key_hint,
		transactions_sheet, wallets_sheet, categories_sheet, enabled, created_by, created_at, updated_at)
		VALUES ('l1', 'o1', 'p1', 'acme', 'home', x'01', 'abcd', 'a', 'b', 'c', 1, 'u1', ?, ?)`, sqliteGrantsTS, sqliteGrantsTS)
	must(`INSERT INTO yasaku_opensheet_sync_state (org_id, project_id, entity, entity_id, version, synced_version, updated_at) VALUES ('o1', 'p1', 'wallet', 'w1', 3, 2, ?)`, sqliteGrantsTS)

	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}

	var version, synced int
	if err := db.QueryRowContext(ctx, `SELECT version, synced_version FROM yasaku_opensheet_sync_state WHERE entity_id = 'w1'`).Scan(&version, &synced); err != nil {
		t.Fatalf("the sync state must survive the links rebuild: %v", err)
	}
	if version != 3 || synced != 2 {
		t.Errorf("sync state = %d/%d, want 3/2", version, synced)
	}
	var credential string
	var enabled int
	if err := db.QueryRowContext(ctx, `SELECT credential, enabled FROM yasaku_opensheet_links WHERE id = 'l1'`).Scan(&credential, &enabled); err != nil {
		t.Fatalf("link after rebuild: %v", err)
	}
	if credential != "api_key" || enabled != 1 {
		t.Errorf("link = %s/%d, want api_key/1", credential, enabled)
	}
	var fkErrors int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_check`).Scan(&fkErrors); err != nil || fkErrors != 0 {
		t.Errorf("foreign_key_check after rebuild = %d, %v", fkErrors, err)
	}
	if _, ok := sqliteIndexes(ctx, t, db, "yasaku_opensheet_links")["yasaku_opensheet_links_enabled_idx"]; !ok {
		t.Error("the enabled index must survive the rebuild")
	}

	link := `INSERT INTO yasaku_opensheet_links (id, org_id, project_id, os_org, os_project, api_key_sealed,
		transactions_sheet, wallets_sheet, categories_sheet, created_by, created_by_key_id, credential, created_at, updated_at)
		VALUES (?, 'o1', ?, 'acme', 'home', ?, 'a', 'b', 'c', ?, NULL, ?, '` + sqliteGrantsTS + `', '` + sqliteGrantsTS + `')`
	if err := exec(link, "l2", "p2", nil, "u1", "api_key"); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("an api_key link without a key must be refused by a CHECK, got %v", err)
	}
	if err := exec(link, "l2", "p2", nil, nil, "grant"); err == nil {
		t.Error("a link with no author must still be refused")
	}
	if err := exec(link, "l2", "p2", nil, "u1", "other"); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("an unknown credential must be refused by a CHECK, got %v", err)
	}
	must(link, "l2", "p2", nil, "u1", "grant")
	must(`INSERT INTO yasaku_opensheet_sync_state (org_id, project_id, entity, entity_id, version, updated_at) VALUES ('o1', 'p2', 'wallet', 'w2', 1, ?)`, sqliteGrantsTS)

	grant := `INSERT INTO yasaku_opensheet_grants (org_id, project_id, connected_by, refresh_sealed, scopes, status, created_at, updated_at)
		VALUES ('o1', ?, 'u1', x'01', 'sheets:read', ?, '` + sqliteGrantsTS + `', '` + sqliteGrantsTS + `')`
	must(grant, "p2", "ok")
	if err := exec(grant, "p2", "ok"); err == nil {
		t.Error("a second grant for one project must be refused")
	}
	if err := exec(grant, "p3", "lost"); err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("an unknown status must be refused by a CHECK, got %v", err)
	}

	if err := MigrateDownTo(ctx, db, cfg, 21); err != nil {
		t.Fatalf("MigrateDownTo 21: %v", err)
	}
	if err := exec(`SELECT 1 FROM yasaku_opensheet_grants`); err == nil {
		t.Error("down must drop opensheet_grants")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM yasaku_opensheet_sync_state WHERE entity_id = 'w1'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("down must keep the key link's sync state: %d, %v", n, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM yasaku_opensheet_links WHERE id = 'l2'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("down must drop the grant link, which has no key: %d, %v", n, err)
	}
}
