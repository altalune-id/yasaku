//go:build integration

package category_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/nanoid"
	"altalune.id/yasaku/schema"
)

type pgFixture struct {
	store  category.Store
	sqlDB  *sql.DB
	prefix string
	tc     tenant.Context
}

func newPgFixture(t *testing.T) pgFixture {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true

	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	userID, orgID := seedPgUserAndOrg(t, sqlDB, prefix)
	projID := seedPgProject(t, sqlDB, prefix, userID, orgID, "web")

	pc := tenant.NewPgConn(sqlDB)
	store := category.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		pc,
	)
	return pgFixture{
		store:  store,
		sqlDB:  sqlDB,
		prefix: prefix,
		tc:     tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID},
	}
}

func seedPgUserAndOrg(t *testing.T, sqlDB *sql.DB, prefix string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	userID, orgID := uuid.New(), uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) "+
			"VALUES ($1, $2, '', '', false, $3, $3)",
		userID, userID.String()+"@example.com", now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, 'Org', $3, $4, $4)",
		orgID, orgID.String()[:8], userID, now)
	require.NoError(t, err)
	return userID, orgID
}

func seedPgProject(t *testing.T, sqlDB *sql.DB, prefix string, userID, orgID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	projID := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $3, $4, $5, $5)",
		projID, orgID, slug, userID, now)
	require.NoError(t, err)
	return projID
}

func TestPostgres_Category_SaveAndByID(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	c, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, c))

	got, err := f.store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, c.ID, got.ID)
	assert.Equal(t, "Release Notes", got.Name)
	assert.Equal(t, "release-notes", got.Slug)
	assert.Equal(t, f.tc.ProjectID, got.ProjectID)
}

func TestPostgres_Category_ByID_NotFound(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	_, err := f.store.ByID(ctx, uuid.New())
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

func TestPostgres_Category_List_OrdersNewestFirst(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	base := time.Now().UTC().Add(-time.Hour)
	for i, name := range []string{"oldest", "middle", "newest"} {
		c, err := category.New(f.tc.OrgID, f.tc.ProjectID, name, "")
		require.NoError(t, err)
		c.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		c.UpdatedAt = c.CreatedAt
		require.NoError(t, f.store.Save(ctx, c))
	}

	got, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"newest", "middle", "oldest"},
		[]string{got[0].Name, got[1].Name, got[2].Name})
}

func TestPostgres_Category_DuplicateSlugInSameProject(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	first, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, first))

	second, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	assert.True(t, category.IsAlreadyExistsError(f.store.Save(ctx, second)),
		"a repeated slug in one project must be AlreadyExistsError")
}

func TestPostgres_Category_SameSlugInAnotherProject(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	first, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, first))

	otherProj := seedPgProject(t, f.sqlDB, f.prefix, f.tc.UserID, f.tc.OrgID, "docs")
	second, err := category.New(f.tc.OrgID, otherProj, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, second), "UNIQUE is (project_id, slug), not slug alone")
}

func TestPostgres_Category_ByIDs(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	a, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Alpha", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, a))

	missing := uuid.New()
	got, err := f.store.ByIDs(ctx, f.tc.OrgID, f.tc.ProjectID, []uuid.UUID{a.ID, missing})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Alpha", got[a.ID].Name)
	assert.NotContains(t, got, missing)

	empty, err := f.store.ByIDs(ctx, f.tc.OrgID, f.tc.ProjectID, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestPostgres_Category_Delete(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	c, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Gone", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, c))
	require.NoError(t, f.store.Delete(ctx, c.ID))

	_, err = f.store.ByID(ctx, c.ID)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError after delete, got %T: %v", err, err)
	assert.True(t, category.IsNotFoundError(f.store.Delete(ctx, uuid.New())))
}

func TestPostgres_Category_Delete_InUse(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	c, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Has Posts", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, c))

	// NOTE: raw INSERT because the post store does not exist yet; this test only
	// needs a referencing row so ON DELETE RESTRICT fires.
	now := time.Now().UTC()
	_, err = f.sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"blog_posts "+
			"(id, org_id, project_id, category_id, title, slug, body_markdown, status, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, 'T', 't', '', 'draft', $5, $5)",
		uuid.New(), f.tc.OrgID, f.tc.ProjectID, c.ID, now)
	require.NoError(t, err)

	err = f.store.Delete(ctx, c.ID)
	require.True(t, category.IsInUseError(err), "want InUseError, got %T: %v", err, err)

	got, err := f.store.ByID(ctx, c.ID)
	require.NoError(t, err, "a refused delete must leave the row in place")
	assert.Equal(t, c.ID, got.ID)
}

// TestPostgres_Category_OtherOrgIsInvisible runs on the RLS fixture, not the superuser
// fixture, so it proves tenant isolation rather than only the store's own org_id predicate.
func TestPostgres_Category_OtherOrgIsInvisible(t *testing.T) {
	store, migDB, prefix := newPgRLSFixture(t)
	a := seedRLSTenant(t, migDB, prefix)
	b := seedRLSTenant(t, migDB, prefix)

	ownerCtx := tenant.Into(t.Context(), a)
	otherCtx := tenant.Into(t.Context(), b)

	c, err := category.New(a.OrgID, a.ProjectID, "Org A Only", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ownerCtx, c))

	_, err = store.ByID(otherCtx, c.ID)
	assert.True(t, category.IsNotFoundError(err), "org B must not see org A's category, got %T: %v", err, err)

	listed, err := store.List(otherCtx, b.OrgID, b.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, listed)

	byIDs, err := store.ByIDs(otherCtx, a.OrgID, a.ProjectID, []uuid.UUID{c.ID})
	require.NoError(t, err)
	assert.Empty(t, byIDs, "org B must not read org A's rows even when it names org A's scope")

	assert.True(t, category.IsNotFoundError(store.Delete(otherCtx, c.ID)),
		"org B must not be able to delete org A's category")

	// Two hijack shapes, refused by two different layers. Carrying org A's org_id trips the
	// policy's WITH CHECK (SQLSTATE 42501); carrying org B's own org_id passes WITH CHECK and
	// is stopped by Save's conflict-clause org guard instead. Either way the row is untouched.
	verbatim := *c
	verbatim.Name = "Hijacked"
	assert.Error(t, store.Save(otherCtx, &verbatim),
		"org B must not be able to upsert onto org A's row")

	reskinned := *c
	reskinned.OrgID, reskinned.ProjectID = b.OrgID, b.ProjectID
	reskinned.Name = "Hijacked"
	assert.Error(t, store.Save(otherCtx, &reskinned),
		"org B must not be able to upsert onto org A's row id under its own org")

	stillThere, err := store.ByID(ownerCtx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, c.ID, stillThere.ID)
	assert.Equal(t, "Org A Only", stillThere.Name, "org A's row must be untouched")
	assert.Equal(t, a.OrgID, stillThere.OrgID, "org A's row must still belong to org A")
}

// newPgRLSFixture migrates under a BYPASSRLS owner with Tenant.RLSEnforce on, then binds
// the store to a NOBYPASSRLS LOGIN role so the tenant policies actually apply. newPgFixture
// above connects as the container superuser, which bypasses row level security outright.
func newPgRLSFixture(t *testing.T) (category.Store, *sql.DB, string) {
	t.Helper()
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	ownerRole := "yasaku_catowner_" + suffix
	appRole := "yasaku_catapp_" + suffix
	prefix := "c" + suffix + "_"

	admin, err := db.Open(t.Context(), db.DBConfig{Driver: db.DriverPostgres, DSN: h.DSN}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })

	createRole(t, admin, ownerRole, "NOLOGIN BYPASSRLS")
	_, err = admin.ExecContext(t.Context(), fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA public TO %q`, ownerRole))
	require.NoError(t, err)

	createRole(t, admin, appRole, "LOGIN PASSWORD 'pw' NOBYPASSRLS")
	_, err = admin.ExecContext(t.Context(), fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %q`, appRole))
	require.NoError(t, err)

	for _, stmt := range []string{
		`ALTER DEFAULT PRIVILEGES FOR ROLE %[1]q IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %[2]q`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE %[1]q IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO %[2]q`,
	} {
		_, err = admin.ExecContext(t.Context(), fmt.Sprintf(stmt, ownerRole, appRole))
		require.NoError(t, err)
	}

	migDB, err := db.Open(t.Context(), db.DBConfig{
		Driver: db.DriverPostgres, DSN: h.DSN, Role: ownerRole, MaxOpenConns: 1,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migDB.Close() })

	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.Schema = "public"
	cfg.DB.TablePrefix = prefix
	cfg.DB.AllowBypassRLS = false
	cfg.Tenant.RLSEnforce = true
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))
	requirePoliciesExist(t, migDB, prefix+"blog_categories")

	appConn, err := db.Open(t.Context(), db.DBConfig{
		Driver: db.DriverPostgres, DSN: pgtest.DSNWithUser(t, h.DSN, appRole, "pw"), MaxOpenConns: 2,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = appConn.Close() })

	store := category.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: "public", TablePrefix: prefix},
		db.Pool{W: appConn, R: appConn},
		tenant.NewPgConn(appConn),
	)
	return store, migDB, prefix
}

// requirePoliciesExist fails loudly when RLSEnforce did not actually render, so a
// cross-org test can never pass merely because no policy was created.
func requirePoliciesExist(t *testing.T, migDB *sql.DB, table string) {
	t.Helper()
	var relRLS, relForce bool
	require.NoError(t, migDB.QueryRowContext(t.Context(),
		`SELECT relrowsecurity, relforcerowsecurity FROM pg_class
		 WHERE oid = ('public.' || $1)::regclass`, table).Scan(&relRLS, &relForce))
	require.True(t, relRLS, "row level security not enabled on %s", table)
	require.True(t, relForce, "row level security not forced on %s", table)

	var policies int
	require.NoError(t, migDB.QueryRowContext(t.Context(),
		`SELECT count(*) FROM pg_policies WHERE schemaname = 'public' AND tablename = $1`,
		table).Scan(&policies))
	require.Positive(t, policies, "no tenant policy on %s", table)
}

func uniqueSuffix(t *testing.T) string {
	t.Helper()
	s, err := nanoid.New(10)
	require.NoError(t, err)
	return strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(s))
}

func createRole(t *testing.T, admin *sql.DB, name, attrs string) {
	t.Helper()
	_, err := admin.ExecContext(t.Context(), fmt.Sprintf(`CREATE ROLE %q %s`, name, attrs))
	require.NoError(t, err)
	// NOTE: t.Context() is already canceled by the time cleanups run, so teardown needs its own context.
	t.Cleanup(func() {
		_, dropErr := admin.ExecContext(context.Background(), fmt.Sprintf(`DROP OWNED BY %q`, name))
		require.NoError(t, dropErr, "leaked objects owned by %s", name)
		_, dropErr = admin.ExecContext(context.Background(), fmt.Sprintf(`DROP ROLE IF EXISTS %q`, name))
		require.NoError(t, dropErr, "leaked role %s", name)
	})
	_, err = admin.ExecContext(t.Context(), fmt.Sprintf(`GRANT %q TO CURRENT_USER`, name))
	require.NoError(t, err)
}

func seedRLSTenant(t *testing.T, migDB *sql.DB, prefix string) tenant.Context {
	t.Helper()
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	_, err := migDB.ExecContext(t.Context(),
		"INSERT INTO public."+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) "+
			"VALUES ($1, $2, '', '', false, $3, $3)",
		userID, userID.String()+"@example.com", now)
	require.NoError(t, err)
	_, err = migDB.ExecContext(t.Context(),
		"INSERT INTO public."+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, 'Org', $3, $4, $4)",
		orgID, orgID.String()[:8], userID, now)
	require.NoError(t, err)
	_, err = migDB.ExecContext(t.Context(),
		"INSERT INTO public."+prefix+"memberships (id, org_id, user_id, role, created_at) VALUES ($1, $2, $3, 'owner', $4)",
		uuid.New(), orgID, userID, now)
	require.NoError(t, err)
	_, err = migDB.ExecContext(t.Context(),
		"INSERT INTO public."+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, 'web', 'Web', $3, $4, $4)",
		projID, orgID, userID, now)
	require.NoError(t, err)
	return tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}
}

// TestPostgres_Category_Save_CannotUpsertOntoAnotherOrgsRow runs on the plain superuser
// fixture ON PURPOSE. RLS is bypassed there, so the conflict-clause org guard in Save is the
// only thing preventing the hijack — which is what makes this test able to detect its removal.
func TestPostgres_Category_Save_CannotUpsertOntoAnotherOrgsRow(t *testing.T) {
	f := newPgFixture(t)
	ownerCtx := tenant.Into(t.Context(), f.tc)

	c, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Org A Only", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ownerCtx, c))

	otherUser, otherOrg := seedPgUserAndOrg(t, f.sqlDB, f.prefix)
	otherProj := seedPgProject(t, f.sqlDB, f.prefix, otherUser, otherOrg, "web")
	otherCtx := tenant.Into(t.Context(), tenant.Context{OrgID: otherOrg, ProjectID: otherProj, UserID: otherUser})

	verbatim := *c
	verbatim.Name = "Hijacked"
	assert.True(t, category.IsNotFoundError(f.store.Save(otherCtx, &verbatim)),
		"org B must not be able to upsert onto org A's row")

	// The handler-shaped attack: org B's own tenant scope on the struct, org A's row id.
	reskinned := *c
	reskinned.OrgID, reskinned.ProjectID = otherOrg, otherProj
	reskinned.Name = "Hijacked"
	assert.True(t, category.IsNotFoundError(f.store.Save(otherCtx, &reskinned)),
		"org B must not be able to upsert onto org A's row id under its own org")

	stillThere, err := f.store.ByID(ownerCtx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "Org A Only", stillThere.Name, "org A's row must be untouched")
	assert.Equal(t, f.tc.OrgID, stillThere.OrgID)
}

// TestPostgres_Category_Save_UpdatesOwnRow guards the other direction: the org guard must not
// break a legitimate upsert by the owning tenant.
func TestPostgres_Category_Save_UpdatesOwnRow(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	c, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, c))

	require.NoError(t, c.Rename("Changelog"))
	require.NoError(t, f.store.Save(ctx, c), "the owning tenant must still be able to update its own row")

	got, err := f.store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "Changelog", got.Name)
	assert.Equal(t, "release-notes", got.Slug)
}
