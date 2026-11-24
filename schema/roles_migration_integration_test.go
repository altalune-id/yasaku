//go:build integration

package schema_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

type pgRolesFixture struct {
	db     *sql.DB
	prefix string
	user   uuid.UUID
}

func newPgRolesFixture(t *testing.T, name string) pgRolesFixture {
	t.Helper()
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	f := pgRolesFixture{prefix: "t" + suffix + "_", user: uuid.New()}
	f.db = migrationRoleDB(t, h, "yasaku_"+name+"_"+suffix, "BYPASSRLS")
	require.NoError(t, schema.MigrateUp(t.Context(), f.db, migrationConfig(f.prefix)))
	f.exec(t, `INSERT INTO `+f.tbl("users")+` (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, now(), now())`,
		f.user, f.user.String()+"@x.co")
	return f
}

func (f pgRolesFixture) tbl(name string) string { return "public." + f.prefix + name }

func (f pgRolesFixture) exec(t *testing.T, q string, args ...any) error {
	t.Helper()
	_, err := f.db.ExecContext(t.Context(), q, args...)
	return err
}

func (f pgRolesFixture) org(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, f.exec(t, `INSERT INTO `+f.tbl("orgs")+` (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'O', $3, now(), now())`,
		id, "o-"+id.String(), f.user))
	return id
}

func (f pgRolesFixture) role(t *testing.T, org uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, f.exec(t, `INSERT INTO `+f.tbl("roles")+` (id, org_id, name, permissions, created_at, updated_at) VALUES ($1, $2, $3, '{wallets:read}', now(), now())`,
		id, org, "r-"+id.String()[:8]))
	return id
}

func (f pgRolesFixture) member(t *testing.T, org uuid.UUID, role string, roleID *uuid.UUID) error {
	t.Helper()
	return f.exec(t, `INSERT INTO `+f.tbl("memberships")+` (id, org_id, user_id, role, role_id, created_at) VALUES ($1, $2, $3, $4, $5, now())`,
		uuid.New(), org, f.user, role, roleID)
}

func (f pgRolesFixture) invite(t *testing.T, org uuid.UUID, role string, roleID *uuid.UUID) error {
	t.Helper()
	return f.exec(t, `INSERT INTO `+f.tbl("invites")+` (id, org_id, email, role, role_id, token_hash, expires_at, invited_by, created_at) VALUES ($1, $2, 'i@x.co', $3, $4, $5, now(), $6, now())`,
		uuid.New(), org, role, roleID, uuid.NewString(), f.user)
}

func requirePgCode(t *testing.T, err error, code, msg string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "%s: want SQLSTATE %s, got %v", msg, code, err)
	assert.Equal(t, code, pgErr.Code, "%s: %s", msg, pgErr.Message)
}

func TestMigration024_ExistingRowsValid(t *testing.T) {
	f := newPgRolesFixture(t, "roles_existing")
	cfg := migrationConfig(f.prefix)
	require.NoError(t, schema.MigrateDownTo(t.Context(), f.db, cfg, 23))

	org := f.org(t)
	for _, r := range []string{"owner", "admin", "member"} {
		require.NoError(t, f.exec(t, `INSERT INTO `+f.tbl("invites")+` (id, org_id, email, role, token_hash, expires_at, invited_by, created_at) VALUES ($1, $2, 'i@x.co', $3, $4, now(), $5, now())`,
			uuid.New(), org, r, uuid.NewString(), f.user))
	}
	require.NoError(t, f.exec(t, `INSERT INTO `+f.tbl("memberships")+` (id, org_id, user_id, role, created_at) VALUES ($1, $2, $3, 'owner', now())`,
		uuid.New(), org, f.user))

	require.NoError(t, schema.MigrateUp(t.Context(), f.db, cfg))
	var members, invites int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+f.tbl("memberships")+` WHERE org_id = $1 AND role_id IS NULL`, org).Scan(&members))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+f.tbl("invites")+` WHERE org_id = $1 AND role_id IS NULL`, org).Scan(&invites))
	assert.Equal(t, 1, members, "built-in memberships survive with no role_id")
	assert.Equal(t, 3, invites, "built-in invites survive with no role_id")
}

func TestMigration024_CustomNeedsRoleID(t *testing.T) {
	f := newPgRolesFixture(t, "roles_custom")
	org := f.org(t)
	role := f.role(t, org)

	requirePgCode(t, f.member(t, org, "custom", nil), "23514", "custom membership with no role_id")
	requirePgCode(t, f.member(t, org, "member", &role), "23514", "built-in membership with a role_id")
	requirePgCode(t, f.member(t, org, "viewer", nil), "23514", "unknown role")
	requirePgCode(t, f.invite(t, org, "custom", nil), "23514", "custom invite with no role_id")
	requirePgCode(t, f.invite(t, org, "admin", &role), "23514", "built-in invite with a role_id")
	require.NoError(t, f.member(t, org, "custom", &role))
	require.NoError(t, f.invite(t, org, "custom", &role))

	requirePgCode(t, f.exec(t, `INSERT INTO `+f.tbl("roles")+` (id, org_id, name, permissions, created_at, updated_at) SELECT $1, org_id, upper(name), permissions, now(), now() FROM `+f.tbl("roles")+` WHERE id = $2`,
		uuid.New(), role), "23505", "role names are unique per org, ignoring case")
}

func TestMigration024_CrossOrgRoleRefused(t *testing.T) {
	f := newPgRolesFixture(t, "roles_xorg")
	orgA, orgB := f.org(t), f.org(t)
	roleA := f.role(t, orgA)

	requirePgCode(t, f.member(t, orgB, "custom", &roleA), "23503", "membership naming another org's role")
	requirePgCode(t, f.invite(t, orgB, "custom", &roleA), "23503", "invite naming another org's role")

	require.NoError(t, f.member(t, orgA, "custom", &roleA))
	requirePgCode(t, f.exec(t, `DELETE FROM `+f.tbl("roles")+` WHERE id = $1`, roleA), "23503", "deleting a held role")

	require.NoError(t, f.exec(t, `DELETE FROM `+f.tbl("orgs")+` WHERE id = $1`, orgA), "deleting the org cascades through its roles and custom members")
	var left int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+f.tbl("roles")+` WHERE org_id = $1`, orgA).Scan(&left))
	assert.Zero(t, left)
}

func TestMigration024_DefinersRevokedFromPublic(t *testing.T) {
	f := newPgRolesFixture(t, "roles_definers")
	const result = "TABLE(id uuid, org_id uuid, email text, role text, role_id uuid, token_hash text, expires_at timestamp with time zone, accepted_at timestamp with time zone, created_at timestamp with time zone)"
	for name, arg := range map[string]string{"resolve_invite_by_token_hash": "text", "list_pending_invites_for_email": "text"} {
		var got string
		var secdef bool
		require.NoError(t, f.db.QueryRowContext(t.Context(),
			`SELECT p.prosecdef, pg_get_function_result(p.oid)
			   FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			  WHERE n.nspname = 'public' AND p.proname = $1`, f.prefix+name).Scan(&secdef, &got))
		assert.True(t, secdef, "%s stays SECURITY DEFINER", name)
		assert.Equal(t, result, got, "%s returns role_id", name)

		var publicExec bool
		require.NoError(t, f.db.QueryRowContext(t.Context(),
			`SELECT has_function_privilege('public', $1, 'EXECUTE')`, "public."+f.prefix+name+"("+arg+")").Scan(&publicExec))
		assert.False(t, publicExec, "PUBLIC must not regain EXECUTE on %s", name)
	}

	var rls, force bool
	require.NoError(t, f.db.QueryRowContext(t.Context(),
		`SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`, f.tbl("roles")).Scan(&rls, &force))
	assert.True(t, rls && force, "roles carries FORCE RLS")

	require.NoError(t, schema.MigrateDownTo(t.Context(), f.db, migrationConfig(f.prefix), 23))
	var got string
	require.NoError(t, f.db.QueryRowContext(t.Context(),
		`SELECT pg_get_function_result(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		  WHERE n.nspname = 'public' AND p.proname = $1`, f.prefix+"resolve_invite_by_token_hash").Scan(&got))
	assert.NotContains(t, got, "role_id", "rollback restores the old definer")
}
