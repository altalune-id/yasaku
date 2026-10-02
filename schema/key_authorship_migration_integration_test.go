//go:build integration

package schema_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestMigrate_Postgres_KeyAuthorshipRoundTripsFromYasaku007(t *testing.T) {
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	prefix := "t" + suffix + "_"
	migDB := migrationRoleDB(t, h, "yasaku_keyauthor_"+suffix, "BYPASSRLS")
	cfg := migrationConfig(prefix)
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))
	require.NoError(t, schema.MigrateDownTo(t.Context(), migDB, cfg, 7))

	exec := func(q string) {
		t.Helper()
		_, err := migDB.ExecContext(t.Context(), q)
		require.NoError(t, err, q)
	}
	const (
		user    = "'00000000-0000-0000-0000-000000000001'"
		org     = "'00000000-0000-0000-0000-0000000000a1'"
		project = "'00000000-0000-0000-0000-0000000000b1'"
		wallet  = "'00000000-0000-0000-0000-0000000000d1'"
		period  = "'00000000-0000-0000-0000-0000000000e1'"
		key     = "'00000000-0000-0000-0000-0000000000c1'"
		t0      = "'2026-01-01T00:00:00Z'"
	)
	tbl := func(name string) string { return "public." + prefix + name }
	exec(`INSERT INTO ` + tbl("users") + ` (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (` + user + `, 'u@x.co', '', '', false, ` + t0 + `, ` + t0 + `)`)
	exec(`INSERT INTO ` + tbl("orgs") + ` (id, slug, name, created_by, created_at, updated_at) VALUES (` + org + `, 'o', 'O', ` + user + `, ` + t0 + `, ` + t0 + `)`)
	exec(`INSERT INTO ` + tbl("projects") + ` (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (` + project + `, ` + org + `, 'p', 'P', ` + user + `, ` + t0 + `, ` + t0 + `)`)
	exec(`INSERT INTO ` + tbl("wallets") + ` (id, org_id, project_id, name, kind, currency, created_at, updated_at) VALUES (` + wallet + `, ` + org + `, ` + project + `, 'Cash', 'cash', 'IDR', ` + t0 + `, ` + t0 + `)`)
	exec(`INSERT INTO ` + tbl("periods") + ` (id, org_id, project_id, name, start_date, end_date, status, created_at, updated_at) VALUES (` + period + `, ` + org + `, ` + project + `, 'Jan', '2026-01-01', '2026-01-31', 'closed', ` + t0 + `, ` + t0 + `)`)
	exec(`INSERT INTO ` + tbl("period_closings") + ` (id, org_id, project_id, period_id, closed_at, closed_by, snapshot) VALUES (gen_random_uuid(), ` + org + `, ` + project + `, ` + period + `, ` + t0 + `, ` + user + `, '{}')`)
	exec(`INSERT INTO ` + tbl("transactions") + ` (id, org_id, project_id, wallet_id, kind, amount_minor, currency, period_id, occurred_at, created_by, created_at, updated_at) VALUES (gen_random_uuid(), ` + org + `, ` + project + `, ` + wallet + `, 'opening', 100, 'IDR', ` + period + `, ` + t0 + `, ` + user + `, ` + t0 + `, ` + t0 + `)`)

	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))

	author := func(q string) (sql.NullString, sql.NullString) {
		t.Helper()
		var by, k sql.NullString
		require.NoError(t, migDB.QueryRowContext(t.Context(), q).Scan(&by, &k))
		return by, k
	}
	by, k := author(`SELECT created_by::text, created_by_key_id::text FROM ` + tbl("transactions"))
	require.Equal(t, "00000000-0000-0000-0000-000000000001", by.String)
	require.False(t, k.Valid, "an existing row must have no key author")
	by, k = author(`SELECT closed_by::text, closed_by_key_id::text FROM ` + tbl("period_closings"))
	require.Equal(t, "00000000-0000-0000-0000-000000000001", by.String)
	require.False(t, k.Valid, "an existing closing must have no key author")

	exec(`INSERT INTO ` + tbl("api_keys") + ` (id, org_id, project_id, name, secret_hash, created_at) VALUES (` + key + `, ` + org + `, ` + project + `, 'ci', '\x01', ` + t0 + `)`)
	exec(`INSERT INTO ` + tbl("transactions") + ` (id, org_id, project_id, wallet_id, kind, amount_minor, currency, occurred_at, created_by, created_by_key_id, created_at, updated_at) VALUES ('00000000-0000-0000-0000-0000000000f1', ` + org + `, ` + project + `, ` + wallet + `, 'adjustment_in', 5, 'IDR', ` + t0 + `, NULL, ` + key + `, ` + t0 + `, ` + t0 + `)`)
	_, err := migDB.ExecContext(t.Context(), `INSERT INTO `+tbl("transactions")+` (id, org_id, project_id, wallet_id, kind, amount_minor, currency, occurred_at, created_by, created_by_key_id, created_at, updated_at) VALUES (gen_random_uuid(), `+org+`, `+project+`, `+wallet+`, 'adjustment_in', 5, 'IDR', `+t0+`, `+user+`, `+key+`, `+t0+`, `+t0+`)`)
	require.Error(t, err, "a row naming both a user and a key must be refused")
	_, err = migDB.ExecContext(t.Context(), `INSERT INTO `+tbl("period_closings")+` (id, org_id, project_id, period_id, closed_at, closed_by, closed_by_key_id, snapshot) VALUES (gen_random_uuid(), `+org+`, `+project+`, `+period+`, `+t0+`, NULL, NULL, '{}')`)
	require.Error(t, err, "a closing naming no author must be refused")

	require.Error(t, schema.MigrateDownTo(t.Context(), migDB, cfg, 15), "rolling back over a key-authored row must fail, not drop the row")
	exec(`DELETE FROM ` + tbl("transactions") + ` WHERE id = '00000000-0000-0000-0000-0000000000f1'`)
	require.NoError(t, schema.MigrateDownTo(t.Context(), migDB, cfg, 7))
	var n int
	require.NoError(t, migDB.QueryRowContext(t.Context(),
		`SELECT (SELECT count(*) FROM `+tbl("transactions")+`) + (SELECT count(*) FROM `+tbl("period_closings")+`)`).Scan(&n))
	require.Equal(t, 2, n, "the ledger rows must survive the round trip")
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))
}
