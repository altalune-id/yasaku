//go:build integration

package schema_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestMigration026_SheetTitleNullableAndReversible(t *testing.T) {
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	prefix := "t" + suffix + "_"
	db := migrationRoleDB(t, h, "yasaku_title_"+suffix, "BYPASSRLS")
	cfg := migrationConfig(prefix)
	ctx := t.Context()
	require.NoError(t, schema.MigrateUp(ctx, db, cfg))
	tbl := func(name string) string { return "public." + prefix + name }

	user, org, project := uuid.New(), uuid.New(), uuid.New()
	_, err := db.ExecContext(ctx, `INSERT INTO `+tbl("users")+` (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, now(), now())`, user, user.String()+"@x.co")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO `+tbl("orgs")+` (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'O', $3, now(), now())`, org, "o-"+org.String(), user)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO `+tbl("projects")+` (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'P', $4, now(), now())`, project, org, "p-"+project.String(), user)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO `+tbl("opensheet_links")+` (id, org_id, project_id, os_org, os_project, api_key_sealed,
		transactions_sheet, wallets_sheet, categories_sheet, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, 'acme', 'home', '\x01', 'a', 'b', 'c', $4, now(), now())`, uuid.New(), org, project, user)
	require.NoError(t, err, "a key link leaves sheet_title and google_file_id NULL")

	var title, file *string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT sheet_title, google_file_id FROM `+tbl("opensheet_links")+` WHERE project_id = $1`, project).Scan(&title, &file))
	require.Nil(t, title)
	require.Nil(t, file)

	require.NoError(t, schema.MigrateDownTo(ctx, db, cfg, 25))
	require.NoError(t, schema.MigrateUp(ctx, db, cfg))
}
