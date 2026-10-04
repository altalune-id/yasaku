//go:build integration

package schema_test

import (
	"testing"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/stretchr/testify/require"

	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestMigrateUp_Postgres_OpensheetTablesAreForcedRLS(t *testing.T) {
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	prefix := "t" + suffix + "_"
	migDB := migrationRoleDB(t, h, "yasaku_opensheet_"+suffix, "BYPASSRLS")
	cfg := migrationConfig(prefix)
	ctx := t.Context()
	require.NoError(t, schema.MigrateUp(ctx, migDB, cfg))

	for _, name := range []string{prefix + "opensheet_links", prefix + "opensheet_sync_state"} {
		var rls, force bool
		require.NoError(t, migDB.QueryRowContext(ctx, `SELECT c.relrowsecurity, c.relforcerowsecurity FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relname = $1`, name).Scan(&rls, &force), name)
		require.True(t, rls, "%s: row level security must be enabled", name)
		require.True(t, force, "%s: row level security must be forced", name)
	}

	columns := func(table string) []string {
		t.Helper()
		rows, err := migDB.QueryContext(ctx, `SELECT column_name FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 ORDER BY ordinal_position`, table)
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			out = append(out, name)
		}
		require.NoError(t, rows.Err())
		return out
	}
	names := func(cols postgres.ColumnList) []string {
		out := make([]string, len(cols))
		for i, c := range cols {
			out[i] = c.Name()
		}
		return out
	}
	require.Equal(t, columns(prefix+"opensheet_links"), names(pgent.NewOpensheetLinks("public", prefix).AllColumns),
		"the opensheet_links binding must match the Postgres columns")
	require.Equal(t, columns(prefix+"opensheet_sync_state"), names(pgent.NewOpensheetSyncState("public", prefix).AllColumns),
		"the opensheet_sync_state binding must match the Postgres columns")

	var linkIdx string
	require.NoError(t, migDB.QueryRowContext(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`,
		prefix+"opensheet_sync_state_link_idx").Scan(&linkIdx))
	require.Contains(t, linkIdx, "(org_id, project_id)", "the link FK needs an index so a link delete cascade does not scan")

	exec := func(q string) error {
		_, err := migDB.ExecContext(ctx, q)
		return err
	}
	must := func(q string) {
		t.Helper()
		require.NoError(t, exec(q), q)
	}
	const (
		user     = "'00000000-0000-0000-0000-000000000001'"
		org      = "'00000000-0000-0000-0000-0000000000a1'"
		project  = "'00000000-0000-0000-0000-0000000000b1'"
		project2 = "'00000000-0000-0000-0000-0000000000b2'"
		link     = "'00000000-0000-0000-0000-0000000000c1'"
		t0       = "'2026-01-01T00:00:00Z'"
	)
	tbl := func(name string) string { return "public." + prefix + name }
	must(`INSERT INTO ` + tbl("users") + ` (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (` + user + `, 'u@x.co', '', '', false, ` + t0 + `, ` + t0 + `)`)
	must(`INSERT INTO ` + tbl("orgs") + ` (id, slug, name, created_by, created_at, updated_at) VALUES (` + org + `, 'o', 'O', ` + user + `, ` + t0 + `, ` + t0 + `)`)
	for i, p := range []string{project, project2} {
		slug := []string{"'p'", "'p2'"}[i]
		must(`INSERT INTO ` + tbl("projects") + ` (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (` + p + `, ` + org + `, ` + slug + `, 'P', ` + user + `, ` + t0 + `, ` + t0 + `)`)
	}
	must(`INSERT INTO ` + tbl("opensheet_links") + ` (id, org_id, project_id, os_org, os_project, api_key_sealed, api_key_hint,
		transactions_sheet, wallets_sheet, categories_sheet, created_by, created_at, updated_at)
		VALUES (` + link + `, ` + org + `, ` + project + `, 'acme', 'home', '\x01', 'abcd', 'a', 'b', 'c', ` + user + `, ` + t0 + `, ` + t0 + `)`)
	state := func(p, entityID string) string {
		return `INSERT INTO ` + tbl("opensheet_sync_state") + ` (org_id, project_id, entity, entity_id, version, updated_at)
			VALUES (` + org + `, ` + p + `, 'wallet', ` + entityID + `, 1, ` + t0 + `)`
	}
	must(state(project, "'00000000-0000-0000-0000-0000000000d1'"))
	for _, col := range []string{"synced_version", "attempts"} {
		err := exec(`INSERT INTO ` + tbl("opensheet_sync_state") + ` (org_id, project_id, entity, entity_id, version, ` + col + `, updated_at)
			VALUES (` + org + `, ` + project + `, 'wallet', gen_random_uuid(), 1, -1, ` + t0 + `)`)
		require.ErrorContains(t, err, "check constraint", "a negative %s must be refused", col)
	}
	require.Error(t, exec(state(project2, "'00000000-0000-0000-0000-0000000000d2'")),
		"state for a project without a link must be refused by the composite FK")

	must(`DELETE FROM ` + tbl("opensheet_links") + ` WHERE id = ` + link)
	var n int
	require.NoError(t, migDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tbl("opensheet_sync_state")).Scan(&n))
	require.Zero(t, n, "removing the link must cascade to its state rows")

	require.NoError(t, schema.MigrateDownTo(ctx, migDB, cfg, 16))
	require.NoError(t, migDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname LIKE $1`, prefix+"opensheet_%").Scan(&n))
	require.Zero(t, n, "down must drop every opensheet relation")
}
