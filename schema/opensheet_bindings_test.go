package schema

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/sqlite"
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
)

func sqliteColumns(ctx context.Context, t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func pgNames(cols postgres.ColumnList) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name()
	}
	return out
}

func sqliteNames(cols sqlite.ColumnList) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name()
	}
	return out
}

func TestOpensheetBindings_MatchTheMigratedColumns(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	cfg := config.Defaults()
	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	prefix := cfg.DB.TablePrefix
	links, state := sqliteColumns(ctx, t, db, prefix+"opensheet_links"), sqliteColumns(ctx, t, db, prefix+"opensheet_sync_state")
	for _, tc := range []struct {
		name string
		got  []string
		want []string
	}{
		{"postgres opensheet_links", pgNames(pgent.NewOpensheetLinks("", prefix).AllColumns), links},
		{"postgres opensheet_sync_state", pgNames(pgent.NewOpensheetSyncState("", prefix).AllColumns), state},
		{"sqlite opensheet_links", sqliteNames(sqliteent.NewOpensheetLinks(prefix).AllColumns), links},
		{"sqlite opensheet_sync_state", sqliteNames(sqliteent.NewOpensheetSyncState(prefix).AllColumns), state},
	} {
		if !slices.Equal(tc.got, tc.want) {
			t.Errorf("%s binding columns = %v, migration has %v", tc.name, tc.got, tc.want)
		}
	}
	if got := pgent.NewOpensheetLinks("", "x_").TableName(); got != "x_opensheet_links" {
		t.Errorf("postgres links table name = %q", got)
	}
	if got := sqliteent.NewOpensheetSyncState("x_").TableName(); got != "x_opensheet_sync_state" {
		t.Errorf("sqlite sync state table name = %q", got)
	}
}
