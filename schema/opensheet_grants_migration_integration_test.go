//go:build integration

package schema_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestMigration025_GrantLinkAllowsNoKey(t *testing.T) {
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	prefix := "t" + suffix + "_"
	db := migrationRoleDB(t, h, "yasaku_grants_"+suffix, "BYPASSRLS")
	cfg := migrationConfig(prefix)
	ctx := t.Context()
	require.NoError(t, schema.MigrateUp(ctx, db, cfg))
	require.NoError(t, schema.MigrateDownTo(ctx, db, cfg, 24))
	tbl := func(name string) string { return "public." + prefix + name }

	user, org := uuid.New(), uuid.New()
	projects := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	_, err := db.ExecContext(ctx, `INSERT INTO `+tbl("users")+` (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, now(), now())`, user, user.String()+"@x.co")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO `+tbl("orgs")+` (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'O', $3, now(), now())`, org, "o-"+org.String(), user)
	require.NoError(t, err)
	for _, p := range projects {
		_, err = db.ExecContext(ctx, `INSERT INTO `+tbl("projects")+` (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'P', $4, now(), now())`, p, org, "p-"+p.String(), user)
		require.NoError(t, err)
	}
	keyLink := uuid.New()
	_, err = db.ExecContext(ctx, `INSERT INTO `+tbl("opensheet_links")+` (id, org_id, project_id, os_org, os_project, api_key_sealed,
		transactions_sheet, wallets_sheet, categories_sheet, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, 'acme', 'home', '\x01', 'a', 'b', 'c', $4, now(), now())`, keyLink, org, projects[0], user)
	require.NoError(t, err)

	require.NoError(t, schema.MigrateUp(ctx, db, cfg))

	var credential string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT credential FROM `+tbl("opensheet_links")+` WHERE id = $1`, keyLink).Scan(&credential))
	require.Equal(t, "api_key", credential, "an existing link is a key link")

	link := `INSERT INTO ` + tbl("opensheet_links") + ` (id, org_id, project_id, os_org, os_project, api_key_sealed,
		transactions_sheet, wallets_sheet, categories_sheet, created_by, credential, created_at, updated_at)
		VALUES ($1, $2, $3, 'acme', 'home', $4, 'a', 'b', 'c', $5, $6, now(), now())`
	_, err = db.ExecContext(ctx, link, uuid.New(), org, projects[1], nil, user, "api_key")
	requirePgCode(t, err, "23514", "an api_key link without a key")
	_, err = db.ExecContext(ctx, link, uuid.New(), org, projects[1], []byte{}, user, "api_key")
	requirePgCode(t, err, "23514", "an api_key link with an empty key")
	_, err = db.ExecContext(ctx, link, uuid.New(), org, projects[1], nil, user, "other")
	requirePgCode(t, err, "23514", "an unknown credential")
	_, err = db.ExecContext(ctx, link, uuid.New(), org, projects[1], nil, user, "grant")
	require.NoError(t, err, "a grant link holds no key")

	grant := `INSERT INTO ` + tbl("opensheet_grants") + ` (org_id, project_id, connected_by, refresh_sealed, scopes, status, created_at, updated_at)
		VALUES ($1, $2, $3, '\x01', 'sheets:read', $4, now(), now())`
	_, err = db.ExecContext(ctx, grant, org, projects[1], user, "ok")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, grant, org, projects[1], user, "ok")
	requirePgCode(t, err, "23505", "a second grant for one project")
	_, err = db.ExecContext(ctx, grant, org, projects[2], user, "lost")
	requirePgCode(t, err, "23514", "an unknown grant status")

	var rls, force bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT c.relrowsecurity, c.relforcerowsecurity FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relname = $1`, prefix+"opensheet_grants").Scan(&rls, &force))
	require.True(t, rls && force, "opensheet_grants must force row level security")

	require.NoError(t, schema.MigrateDownTo(ctx, db, cfg, 24))
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM `+tbl("opensheet_links")).Scan(&n))
	require.Equal(t, 1, n, "down keeps the key link and drops the grant link")
}
