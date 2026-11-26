package schema

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
)

func TestMigration023_SheetTitleRoundTrips(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	cfg := config.Defaults()
	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	has := func(col string) bool {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('yasaku_opensheet_links') WHERE name = ?`, col).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	if !has("sheet_title") || !has("google_file_id") {
		t.Fatal("opensheet_links needs sheet_title and google_file_id after 023")
	}
	if err := MigrateDownTo(ctx, db, cfg, 22); err != nil {
		t.Fatalf("MigrateDownTo 22: %v", err)
	}
	if has("sheet_title") || has("google_file_id") {
		t.Fatal("023 down must drop both columns")
	}
	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp again: %v", err)
	}
}
