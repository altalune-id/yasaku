//go:build integration

package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

func TestAdminListOrgs_DefinerRevokedFromPublic(t *testing.T) {
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	role := "yasaku_support_" + suffix
	prefix := "t" + suffix + "_"

	migDB := migrationRoleDB(t, h, role, "BYPASSRLS")
	cfg := migrationConfig(prefix)
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))

	for name, result := range map[string]string{
		"admin_list_orgs":  "TABLE(id uuid, slug text, name text, plan text, created_at timestamp with time zone, member_count bigint, project_count bigint, last_active timestamp with time zone)",
		"admin_list_users": "TABLE(id uuid, email text, name text, created_at timestamp with time zone, last_sign_in_at timestamp with time zone, suspended_at timestamp with time zone, org_count bigint)",
	} {
		var owner, got string
		var secdef bool
		require.NoError(t, migDB.QueryRowContext(t.Context(),
			`SELECT pg_get_userbyid(p.proowner), p.prosecdef, pg_get_function_result(p.oid)
			   FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			  WHERE n.nspname = 'public' AND p.proname = $1`,
			prefix+name).Scan(&owner, &secdef, &got))
		assert.Equal(t, role, owner, "%s must be owned by the migration role", name)
		assert.True(t, secdef, "%s must be SECURITY DEFINER to read across tenants", name)
		assert.Equal(t, result, got, "%s returns counts and identity, never ledger data", name)

		var publicExec bool
		require.NoError(t, migDB.QueryRowContext(t.Context(),
			`SELECT has_function_privilege('public', $1, 'EXECUTE')`,
			"public."+prefix+name+"()").Scan(&publicExec))
		assert.False(t, publicExec, "PUBLIC must not hold EXECUTE on %s", name)
	}

	var cols int
	require.NoError(t, migDB.QueryRowContext(t.Context(),
		`SELECT count(*) FROM information_schema.columns
		  WHERE table_schema = 'public' AND table_name = $1 AND column_name IN ('suspended_at', 'last_sign_in_at')`,
		prefix+"users").Scan(&cols))
	assert.Equal(t, 2, cols)

	require.NoError(t, schema.MigrateDownTo(t.Context(), migDB, cfg, 22))
	require.NoError(t, migDB.QueryRowContext(t.Context(),
		`SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		  WHERE n.nspname = 'public' AND p.proname IN ($1, $2)`,
		prefix+"admin_list_orgs", prefix+"admin_list_users").Scan(&cols))
	assert.Zero(t, cols, "rollback must drop both definers")
}
