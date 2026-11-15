//go:build integration

package schema_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestMigrateUp_Postgres_OpensheetResyncDirtiesEveryOrgsRows(t *testing.T) {
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	prefix := "t" + suffix + "_"
	migDB := migrationRoleDB(t, h, "yasaku_resync_"+suffix, "BYPASSRLS")
	cfg := migrationConfig(prefix)
	require.True(t, cfg.Tenant.RLSEnforce, "the rows sit behind FORCE row level security, as in production")
	ctx := t.Context()
	require.NoError(t, schema.MigrateUp(ctx, migDB, cfg))
	require.NoError(t, schema.MigrateDownTo(ctx, migDB, cfg, 17))

	tbl := func(name string) string { return "public." + prefix + name }
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	user := uuid.New()
	_, err := migDB.ExecContext(ctx, "INSERT INTO "+tbl("users")+" (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1,$2,'','',false,$3,$3)",
		user, user.String()+"@x.co", t0)
	require.NoError(t, err)
	for i := range 2 {
		org, project := uuid.New(), uuid.New()
		_, err := migDB.ExecContext(ctx, "INSERT INTO "+tbl("orgs")+" (id, slug, name, created_by, created_at, updated_at) VALUES ($1,$2,'O',$3,$4,$4)",
			org, "o-"+org.String(), user, t0)
		require.NoError(t, err)
		_, err = migDB.ExecContext(ctx, "INSERT INTO "+tbl("projects")+" (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1,$2,$3,'P',$4,$5,$5)",
			project, org, "p-"+project.String(), user, t0)
		require.NoError(t, err)
		_, err = migDB.ExecContext(ctx, "INSERT INTO "+tbl("opensheet_links")+` (id, org_id, project_id, os_org, os_project, api_key_sealed, api_key_hint,
			transactions_sheet, wallets_sheet, categories_sheet, created_by, created_at, updated_at)
			VALUES ($1,$2,$3,'acme','home','\x01','abcd','a','b','c',$4,$5,$5)`, uuid.New(), org, project, user, t0)
		require.NoError(t, err)
		for _, entity := range []string{"transaction", "wallet", "category"} {
			_, err = migDB.ExecContext(ctx, "INSERT INTO "+tbl("opensheet_sync_state")+` (org_id, project_id, entity, entity_id, version, synced_version, attempts, retry_after, updated_at)
				VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8)`, org, project, entity, uuid.New(), 3+i, 5, t0.Add(time.Hour), t0)
			require.NoError(t, err)
		}
	}

	require.NoError(t, schema.MigrateUp(ctx, migDB, cfg))

	var total, dirty, versionKept, eligible int
	require.NoError(t, migDB.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE synced_version = 0), COUNT(*) FILTER (WHERE version >= 3),
		COUNT(*) FILTER (WHERE attempts = 0 AND retry_after IS NULL)
		FROM `+tbl("opensheet_sync_state")).Scan(&total, &dirty, &versionKept, &eligible))
	require.Equal(t, 6, total)
	require.Equal(t, total, dirty, "the migration role must reach every org's rows through FORCE row level security")
	require.Equal(t, total, versionKept, "version stays so the next mark still bumps it")
	require.Equal(t, total, eligible, "a given-up row gets fresh attempts and no backoff, so the sync takes it again")
}
