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

func seedSQLiteKeyLedger(ctx context.Context, t *testing.T, db *sql.DB, org string) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	const ts = "2026-01-01T00:00:00Z"
	exec(`INSERT INTO yasaku_orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'O', 'u1', ?, ?)`, org, org, ts, ts)
	exec(`INSERT INTO yasaku_projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'P', 'u1', ?, ?)`, org+"-p", org, org+"-p", ts, ts)
	exec(`INSERT INTO yasaku_api_keys (id, org_id, project_id, name, secret_hash, created_at) VALUES (?, ?, ?, 'ci', ?, ?)`, org+"-k", org, org+"-p", []byte(org), ts)
	exec(`INSERT INTO yasaku_wallets (id, org_id, project_id, name, kind, currency, created_at, updated_at) VALUES (?, ?, ?, 'Cash', 'cash', 'IDR', ?, ?)`, org+"-w", org, org+"-p", ts, ts)
	exec(`INSERT INTO yasaku_periods (id, org_id, project_id, name, start_date, end_date, status, created_at, updated_at) VALUES (?, ?, ?, 'Jan', '2026-01-01', '2026-01-31', 'closed', ?, ?)`, org+"-pe", org, org+"-p", ts, ts)
	exec(`INSERT INTO yasaku_period_closings (id, org_id, project_id, period_id, closed_at, closed_by_key_id, snapshot) VALUES (?, ?, ?, ?, ?, ?, '{}')`, org+"-c", org, org+"-p", org+"-pe", ts, org+"-k")
	exec(`INSERT INTO yasaku_transactions (id, org_id, project_id, wallet_id, kind, amount_minor, currency, occurred_at, created_by_key_id, created_at, updated_at) VALUES (?, ?, ?, ?, 'opening', 100, 'IDR', ?, ?, ?, ?)`, org+"-t", org, org+"-p", org+"-w", ts, org+"-k", ts, ts)
}

func TestMigrate_SQLite_KeyAuthorFKRestrictsKeyDeleteButNotCascades(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO yasaku_users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ('u1', 'u1@x.co', '', '', 0, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	count := func(table, org string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE org_id = ?`, org).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	seedSQLiteKeyLedger(ctx, t, db, "o1")
	_, err = db.ExecContext(ctx, `DELETE FROM yasaku_api_keys WHERE id = 'o1-k'`)
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		t.Fatalf("deleting a key that authored ledger rows = %v, want a foreign key violation", err)
	}
	if count("yasaku_transactions", "o1") != 1 || count("yasaku_period_closings", "o1") != 1 {
		t.Fatal("a refused key delete must leave the ledger rows")
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM yasaku_projects WHERE id = 'o1-p'`); err != nil {
		t.Fatalf("deleting the project must cascade through its keys and ledger rows: %v", err)
	}
	for _, tbl := range []string{"yasaku_transactions", "yasaku_period_closings", "yasaku_api_keys"} {
		if n := count(tbl, "o1"); n != 0 {
			t.Fatalf("%s rows after project delete = %d, want 0", tbl, n)
		}
	}

	seedSQLiteKeyLedger(ctx, t, db, "o2")
	if _, err := db.ExecContext(ctx, `DELETE FROM yasaku_orgs WHERE id = 'o2'`); err != nil {
		t.Fatalf("deleting the org must cascade through its keys and ledger rows: %v", err)
	}
	for _, tbl := range []string{"yasaku_transactions", "yasaku_period_closings", "yasaku_api_keys"} {
		if n := count(tbl, "o2"); n != 0 {
			t.Fatalf("%s rows after org delete = %d, want 0", tbl, n)
		}
	}

	if err := MigrateDownTo(ctx, db, cfg, 12); err != nil {
		t.Fatalf("Down must work once no key-authored rows remain: %v", err)
	}
	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp again: %v", err)
	}
}
