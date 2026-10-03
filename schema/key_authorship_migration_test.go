package schema

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
)

func sqliteIndexes(ctx context.Context, t *testing.T, db *sql.DB, table string) map[string]string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type = 'index' AND tbl_name = ?`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatal(err)
		}
		out[name] = ddl
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func sqliteForeignKeys(ctx context.Context, t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT "table", "from", COALESCE("to", ''), on_delete FROM pragma_foreign_key_list(?) ORDER BY id, seq`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var ref, from, to, onDelete string
		if err := rows.Scan(&ref, &from, &to, &onDelete); err != nil {
			t.Fatal(err)
		}
		out = append(out, ref+"."+from+"->"+to+" "+onDelete)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMigrate_SQLite_KeyAuthorshipKeepsRowsIndexesAndKeys(t *testing.T) {
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
	if err := MigrateDownTo(ctx, db, cfg, 5); err != nil {
		t.Fatalf("MigrateDownTo 5: %v", err)
	}

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	const ts = "2026-01-01T00:00:00Z"
	exec(`INSERT INTO yasaku_users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ('u1', 'u1@x.co', '', '', 0, ?, ?)`, ts, ts)
	exec(`INSERT INTO yasaku_orgs (id, slug, name, created_by, created_at, updated_at) VALUES ('o1', 'o1', 'O', 'u1', ?, ?)`, ts, ts)
	exec(`INSERT INTO yasaku_projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ('p1', 'o1', 'p1', 'P', 'u1', ?, ?)`, ts, ts)
	exec(`INSERT INTO yasaku_wallets (id, org_id, project_id, name, kind, currency, created_at, updated_at) VALUES ('w1', 'o1', 'p1', 'Cash', 'cash', 'IDR', ?, ?)`, ts, ts)
	exec(`INSERT INTO yasaku_periods (id, org_id, project_id, name, start_date, end_date, status, created_at, updated_at) VALUES ('pe1', 'o1', 'p1', 'Jan', '2026-01-01', '2026-01-31', 'closed', ?, ?)`, ts, ts)
	exec(`INSERT INTO yasaku_period_closings (id, org_id, project_id, period_id, closed_at, closed_by, snapshot) VALUES ('c1', 'o1', 'p1', 'pe1', ?, 'u1', '{}')`, ts)
	exec(`INSERT INTO yasaku_transactions (id, org_id, project_id, wallet_id, kind, amount_minor, currency, period_id, occurred_at, created_by, created_at, updated_at) VALUES ('t1', 'o1', 'p1', 'w1', 'opening', 100, 'IDR', 'pe1', ?, 'u1', ?, ?)`, ts, ts, ts)

	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp to 13: %v", err)
	}
	if err := MigrateDownTo(ctx, db, cfg, 12); err != nil {
		t.Fatalf("MigrateDownTo 12: %v", err)
	}
	beforeIdx := map[string]map[string]string{}
	beforeFK := map[string][]string{}
	for _, tbl := range []string{"yasaku_transactions", "yasaku_period_closings"} {
		beforeIdx[tbl] = sqliteIndexes(ctx, t, db, tbl)
		beforeFK[tbl] = sqliteForeignKeys(ctx, t, db, tbl)
	}
	if len(beforeIdx["yasaku_transactions"]) != 7 || len(beforeIdx["yasaku_period_closings"]) != 2 {
		t.Fatalf("baseline indexes = %v, want 7 on transactions and 2 on period_closings (with autoindexes)", beforeIdx)
	}

	if err := MigrateUp(ctx, db, cfg); err != nil {
		t.Fatalf("MigrateUp to 13 again: %v", err)
	}

	author := func(q string) (sql.NullString, sql.NullString) {
		t.Helper()
		var by, key sql.NullString
		if err := db.QueryRowContext(ctx, q).Scan(&by, &key); err != nil {
			t.Fatal(err)
		}
		return by, key
	}
	if by, key := author(`SELECT created_by, created_by_key_id FROM yasaku_transactions WHERE id = 't1'`); by.String != "u1" || key.Valid {
		t.Fatalf("transaction author after up = (%v, %v), want (u1, NULL)", by, key)
	}
	if by, key := author(`SELECT closed_by, closed_by_key_id FROM yasaku_period_closings WHERE id = 'c1'`); by.String != "u1" || key.Valid {
		t.Fatalf("closing author after up = (%v, %v), want (u1, NULL)", by, key)
	}
	for tbl, want := range beforeIdx {
		got := sqliteIndexes(ctx, t, db, tbl)
		for name, ddl := range want {
			if got[name] != ddl {
				t.Fatalf("%s index %s after rebuild = %q, want %q", tbl, name, got[name], ddl)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("%s indexes after rebuild = %v, want %v", tbl, got, want)
		}
		fks := sqliteForeignKeys(ctx, t, db, tbl)
		if len(fks) != len(beforeFK[tbl])+1 {
			t.Fatalf("%s foreign keys after rebuild = %v, want %v plus the api_keys one", tbl, fks, beforeFK[tbl])
		}
	}

	exec(`INSERT INTO yasaku_api_keys (id, org_id, project_id, name, secret_hash, created_at) VALUES ('k1', 'o1', 'p1', 'ci', x'01', ?)`, ts)
	exec(`INSERT INTO yasaku_transactions (id, org_id, project_id, wallet_id, kind, amount_minor, currency, occurred_at, created_by, created_by_key_id, created_at, updated_at) VALUES ('t2', 'o1', 'p1', 'w1', 'adjustment_in', 5, 'IDR', ?, NULL, 'k1', ?, ?)`, ts, ts, ts)
	if _, err := db.ExecContext(ctx, `INSERT INTO yasaku_transactions (id, org_id, project_id, wallet_id, kind, amount_minor, currency, occurred_at, created_by, created_by_key_id, created_at, updated_at) VALUES ('t3', 'o1', 'p1', 'w1', 'adjustment_in', 5, 'IDR', ?, 'u1', 'k1', ?, ?)`, ts, ts, ts); err == nil {
		t.Fatal("a row naming both a user and a key must be refused")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO yasaku_period_closings (id, org_id, project_id, period_id, closed_at, closed_by, closed_by_key_id, snapshot) VALUES ('c2', 'o1', 'p1', 'pe1', ?, NULL, NULL, '{}')`, ts); err == nil {
		t.Fatal("a closing naming no author must be refused")
	}
	if err := MigrateDownTo(ctx, db, cfg, 12); err == nil {
		t.Fatal("rolling back over a key-authored row must fail, not drop the row")
	}
	exec(`DELETE FROM yasaku_transactions WHERE id = 't2'`)
	if err := MigrateDownTo(ctx, db, cfg, 12); err != nil {
		t.Fatalf("MigrateDownTo 12 without key rows: %v", err)
	}
	for tbl, want := range beforeIdx {
		if got := sqliteIndexes(ctx, t, db, tbl); len(got) != len(want) {
			t.Fatalf("%s indexes after down = %v, want %v", tbl, got, want)
		}
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM yasaku_transactions) + (SELECT count(*) FROM yasaku_period_closings)`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rows after down = %d, want 2", n)
	}
}
