//go:build integration

package blog_test

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

	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/nanoid"
	"altalune.id/yasaku/schema"
)

type pgFixture struct {
	store  blog.Store
	sqlDB  *sql.DB
	prefix string
	tc     tenant.Context
	cat    uuid.UUID
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

	pfx := cfg.DB.TablePrefix
	tc := seedPgTenant(t, sqlDB, pfx)
	store := blog.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: pfx},
		db.Pool{W: sqlDB, R: sqlDB},
		tenant.NewPgConn(sqlDB),
	)
	return pgFixture{
		store:  store,
		sqlDB:  sqlDB,
		prefix: pfx,
		tc:     tc,
		cat:    seedPgCategory(t, sqlDB, pfx, tc, tc.ProjectID),
	}
}

func seedPgTenant(t *testing.T, sqlDB *sql.DB, prefix string) tenant.Context {
	t.Helper()
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
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
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, $3, 'Web', $4, $5, $5)",
		projID, orgID, projID.String()[:8], userID, now)
	require.NoError(t, err)
	return tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}
}

func seedPgProject(t *testing.T, sqlDB *sql.DB, prefix string, tc tenant.Context) uuid.UUID {
	t.Helper()
	projID := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, $3, 'Docs', $4, $5, $5)",
		projID, tc.OrgID, projID.String()[:8], tc.UserID, now)
	require.NoError(t, err)
	return projID
}

func seedPgCategory(t *testing.T, sqlDB *sql.DB, prefix string, tc tenant.Context, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"blog_categories (id, org_id, project_id, name, slug, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $4, $5, $5)",
		id, tc.OrgID, projectID, id.String()[:8], now)
	require.NoError(t, err)
	return id
}

func seedPgTag(t *testing.T, sqlDB *sql.DB, prefix string, tc tenant.Context) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"blog_tags (id, org_id, project_id, name, slug, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $4, $5, $5)",
		id, tc.OrgID, tc.ProjectID, id.String()[:8], now)
	require.NoError(t, err)
	return id
}

func TestPostgres_Post_SaveAndByID(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	p, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Hello World", "", "# body")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, p))

	got, err := f.store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, p.ID, got.ID)
	assert.Equal(t, "Hello World", got.Title)
	assert.Equal(t, "hello-world", got.Slug)
	assert.Equal(t, blog.StatusDraft, got.Status)
	assert.Nil(t, got.FirstPublishedAt)
	assert.Empty(t, got.TagIDs)
	// NOTE: postgres timestamptz is microsecond precision, so a nanosecond-precision
	// time.Now() does not survive the round trip. On macOS the nanoseconds are often
	// already zero, which hides this locally; linux CI fails it.
	assert.True(t, got.CreatedAt.Equal(p.CreatedAt.Truncate(time.Microsecond)),
		"CreatedAt round-trip: got=%v want=%v", got.CreatedAt, p.CreatedAt.Truncate(time.Microsecond))
}

func TestPostgres_Post_NotFound(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	_, err := f.store.ByID(ctx, uuid.New())
	assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestPostgres_Post_RoundTripsPublication(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	p, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Published", "", "body")
	require.NoError(t, err)
	p.Publish()
	require.NoError(t, f.store.Save(ctx, p))

	got, err := f.store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, blog.StatusPublished, got.Status)
	require.NotNil(t, got.FirstPublishedAt)
	assert.True(t, got.FirstPublishedAt.Equal(p.FirstPublishedAt.Truncate(time.Microsecond)))

	first := *p.FirstPublishedAt
	p.Unpublish()
	require.NoError(t, f.store.Save(ctx, p))

	got, err = f.store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, blog.StatusDraft, got.Status)
	require.NotNil(t, got.FirstPublishedAt, "unpublishing must retain the first publication")
	assert.True(t, got.FirstPublishedAt.Equal(first.Truncate(time.Microsecond)))
}

func TestPostgres_Post_SaveReplacesTagSetWithoutOrphans(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	p, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "First", "", "body")
	require.NoError(t, err)
	a := seedPgTag(t, f.sqlDB, f.prefix, f.tc)
	b := seedPgTag(t, f.sqlDB, f.prefix, f.tc)
	c := seedPgTag(t, f.sqlDB, f.prefix, f.tc)

	p.SetTags([]uuid.UUID{a, b})
	require.NoError(t, f.store.Save(ctx, p))

	p.SetTags([]uuid.UUID{b, c})
	require.NoError(t, f.store.Save(ctx, p))

	got, err := f.store.ByID(ctx, p.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{b, c}, got.TagIDs)

	var rows int
	require.NoError(t, f.sqlDB.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+f.prefix+"blog_post_tags WHERE post_id = $1", p.ID).Scan(&rows))
	assert.Equal(t, 2, rows, "replacing the tag set must leave no orphan join rows")
}

func TestPostgres_Post_SaveWithUnknownTagIsRefusedWhole(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	p, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "First", "", "body")
	require.NoError(t, err)
	good := seedPgTag(t, f.sqlDB, f.prefix, f.tc)
	p.SetTags([]uuid.UUID{good})
	require.NoError(t, f.store.Save(ctx, p))

	p.Title = "Edited"
	p.SetTags([]uuid.UUID{good, uuid.New()})
	require.Error(t, f.store.Save(ctx, p), "an unknown tag id must fail the foreign key")

	got, err := f.store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{good}, got.TagIDs,
		"a failed Save must roll back the tag replacement, not half-apply it")
	assert.Equal(t, "First", got.Title, "the post row must roll back with the join rows")
}

func TestPostgres_Post_DuplicateSlugInOneProject(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	first, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Hello World", "", "body")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, first))

	dup, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "hello   world", "", "body")
	require.NoError(t, err)
	require.Equal(t, first.Slug, dup.Slug)

	err = f.store.Save(ctx, dup)
	assert.True(t, blog.IsAlreadyExistsError(err), "got %T: %v", err, err)
}

func TestPostgres_Post_SameSlugInTwoProjectsIsAllowed(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	otherProj := seedPgProject(t, f.sqlDB, f.prefix, f.tc)
	otherCat := seedPgCategory(t, f.sqlDB, f.prefix, f.tc, otherProj)

	a, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Hello World", "", "body")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, a))

	b, err := blog.New(f.tc.OrgID, otherProj, otherCat, "Hello World", "", "body")
	require.NoError(t, err)
	assert.NoError(t, f.store.Save(ctx, b), "uniqueness is per project, not per org")
}

func TestPostgres_Post_List_OrdersAndFilters(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	otherCat := seedPgCategory(t, f.sqlDB, f.prefix, f.tc, f.tc.ProjectID)

	base := time.Now().UTC().Truncate(time.Second)
	mk := func(title string, created time.Time, cat uuid.UUID) *blog.Post {
		p, err := blog.New(f.tc.OrgID, f.tc.ProjectID, cat, title, "", "body")
		require.NoError(t, err)
		p.CreatedAt = created
		p.UpdatedAt = created
		require.NoError(t, f.store.Save(ctx, p))
		return p
	}
	oldest := mk("oldest", base.Add(-2*time.Hour), f.cat)
	tieA := mk("tie-a", base, f.cat)
	tieB := mk("tie-b", base, otherCat)

	got, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	require.Len(t, got, 3)

	hi, lo := tieA, tieB
	if hi.ID.String() < lo.ID.String() {
		hi, lo = lo, hi
	}
	assert.Equal(t, hi.ID, got[0].ID, "ties on created_at must break on id DESC")
	assert.Equal(t, lo.ID, got[1].ID)
	assert.Equal(t, oldest.ID, got[2].ID)

	byCategory, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, blog.ListOpts{CategoryID: &otherCat})
	require.NoError(t, err)
	require.Len(t, byCategory, 1)
	assert.Equal(t, tieB.ID, byCategory[0].ID)

	published := blog.StatusPublished
	byStatus, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, blog.ListOpts{Status: &published})
	require.NoError(t, err)
	assert.Empty(t, byStatus, "nothing has been published yet")

	tieA.Publish()
	require.NoError(t, f.store.Save(ctx, tieA))
	byStatus, err = f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, blog.ListOpts{Status: &published})
	require.NoError(t, err)
	require.Len(t, byStatus, 1)
	assert.Equal(t, tieA.ID, byStatus[0].ID)
}

func TestPostgres_Post_List_AttachesTagsWithoutMultiplyingRows(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	a := seedPgTag(t, f.sqlDB, f.prefix, f.tc)
	b := seedPgTag(t, f.sqlDB, f.prefix, f.tc)

	tagged, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Tagged", "", "body")
	require.NoError(t, err)
	tagged.SetTags([]uuid.UUID{a, b})
	require.NoError(t, f.store.Save(ctx, tagged))

	bare, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Bare", "", "body")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, bare))

	got, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	require.Len(t, got, 2, "two tags on one post must not yield two rows for it")

	byID := map[uuid.UUID][]uuid.UUID{}
	for _, p := range got {
		byID[p.ID] = p.TagIDs
	}
	assert.ElementsMatch(t, []uuid.UUID{a, b}, byID[tagged.ID])
	assert.Empty(t, byID[bare.ID])
}

func TestPostgres_Post_Delete(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	p, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Gone", "", "body")
	require.NoError(t, err)
	p.SetTags([]uuid.UUID{seedPgTag(t, f.sqlDB, f.prefix, f.tc)})
	require.NoError(t, f.store.Save(ctx, p))

	require.NoError(t, f.store.Delete(ctx, p.ID))

	_, err = f.store.ByID(ctx, p.ID)
	assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
	assert.True(t, blog.IsNotFoundError(f.store.Delete(ctx, p.ID)), "double delete")

	var rows int
	require.NoError(t, f.sqlDB.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+f.prefix+"blog_post_tags WHERE post_id = $1", p.ID).Scan(&rows))
	assert.Zero(t, rows, "deleting a post must take its join rows with it")
}

func TestPostgres_Post_Counts(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	otherCat := seedPgCategory(t, f.sqlDB, f.prefix, f.tc, f.tc.ProjectID)
	a := seedPgTag(t, f.sqlDB, f.prefix, f.tc)
	b := seedPgTag(t, f.sqlDB, f.prefix, f.tc)
	unused := seedPgTag(t, f.sqlDB, f.prefix, f.tc)

	first, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "First", "", "body")
	require.NoError(t, err)
	first.SetTags([]uuid.UUID{a, b})
	require.NoError(t, f.store.Save(ctx, first))

	second, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Second", "", "body")
	require.NoError(t, err)
	second.SetTags([]uuid.UUID{a})
	require.NoError(t, f.store.Save(ctx, second))

	byCategory, err := f.store.CountByCategory(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, 2, byCategory[f.cat])
	_, present := byCategory[otherCat]
	assert.False(t, present, "a category with no posts is absent, not zero-valued")

	byTag, err := f.store.CountByTag(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, 2, byTag[a])
	assert.Equal(t, 1, byTag[b])
	_, present = byTag[unused]
	assert.False(t, present, "a tag with no posts is absent, not zero-valued")
}

// newPgRLSFixture migrates under a BYPASSRLS owner and returns a store bound to a
// NOBYPASSRLS app role, so the row level security policies actually apply. The plain
// fixture above connects as the container superuser, which bypasses RLS outright.
func newPgRLSFixture(t *testing.T) (blog.Store, *sql.DB, *sql.DB, string) {
	t.Helper()
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	ownerRole := "yasaku_postowner_" + suffix
	appRole := "yasaku_postapp_" + suffix
	pfx := "p" + suffix + "_"

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
	cfg.DB.TablePrefix = pfx
	cfg.DB.AllowBypassRLS = false
	cfg.Tenant.RLSEnforce = true
	require.NoError(t, schema.MigrateUp(t.Context(), migDB, cfg))

	appConn, err := db.Open(t.Context(), db.DBConfig{
		Driver: db.DriverPostgres, DSN: pgtest.DSNWithUser(t, h.DSN, appRole, "pw"), MaxOpenConns: 2,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = appConn.Close() })

	store := blog.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: "public", TablePrefix: pfx},
		db.Pool{W: appConn, R: appConn},
		tenant.NewPgConn(appConn),
	)
	return store, migDB, appConn, pfx
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

func seedRLSTenant(t *testing.T, migDB *sql.DB, prefix string) (tenant.Context, uuid.UUID, uuid.UUID) {
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
			"VALUES ($1, $2, $3, 'Web', $4, $5, $5)",
		projID, orgID, projID.String()[:8], userID, now)
	require.NoError(t, err)

	tc := tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}
	catID, tagID := uuid.New(), uuid.New()
	_, err = migDB.ExecContext(t.Context(),
		"INSERT INTO public."+prefix+"blog_categories (id, org_id, project_id, name, slug, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $4, $5, $5)",
		catID, orgID, projID, catID.String()[:8], now)
	require.NoError(t, err)
	_, err = migDB.ExecContext(t.Context(),
		"INSERT INTO public."+prefix+"blog_tags (id, org_id, project_id, name, slug, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, $4, $5, $5)",
		tagID, orgID, projID, tagID.String()[:8], now)
	require.NoError(t, err)
	return tc, catID, tagID
}

// TestPostgres_Post_OtherOrgIsInvisible runs under enforced row level security: the app role
// holds NOBYPASSRLS, so the policies created by the migration actually filter. It covers both
// the store surface and a direct read of the join table as that same role.
func TestPostgres_Post_OtherOrgIsInvisible(t *testing.T) {
	store, migDB, appConn, prefix := newPgRLSFixture(t)
	a, aCat, aTag := seedRLSTenant(t, migDB, prefix)
	b, _, _ := seedRLSTenant(t, migDB, prefix)

	ownerCtx := tenant.Into(t.Context(), a)
	otherCtx := tenant.Into(t.Context(), b)

	p, err := blog.New(a.OrgID, a.ProjectID, aCat, "Org A Only", "", "secret body")
	require.NoError(t, err)
	p.SetTags([]uuid.UUID{aTag})
	require.NoError(t, store.Save(ownerCtx, p))

	_, err = store.ByID(otherCtx, p.ID)
	assert.True(t, blog.IsNotFoundError(err), "org B must not see org A's post, got %T: %v", err, err)

	listed, err := store.List(otherCtx, b.OrgID, b.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, listed)

	named, err := store.List(otherCtx, a.OrgID, a.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, named, "naming org A's scope from org B's context must list nothing")

	counts, err := store.CountByCategory(otherCtx, a.OrgID, a.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, counts, "org B must not count org A's posts")

	tagCounts, err := store.CountByTag(otherCtx, a.OrgID, a.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, tagCounts, "org B must not count org A's join rows")

	assert.True(t, blog.IsNotFoundError(store.Delete(otherCtx, p.ID)),
		"org B must not be able to delete org A's post")

	// SECURITY: the join table carries its own org_id and its own policy; read it directly as
	// the app role under org B's scope to prove the rows are invisible there too.
	tx, err := tenant.NewPgConn(appConn).BeginTenanted(t.Context(), b)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	var joinRows int
	require.NoError(t, tx.QueryRowContext(t.Context(),
		"SELECT count(*) FROM public."+prefix+"blog_post_tags WHERE post_id = $1", p.ID).Scan(&joinRows))
	assert.Zero(t, joinRows, "org A's join rows must be invisible under org B's tenant scope")

	var postRows int
	require.NoError(t, tx.QueryRowContext(t.Context(),
		"SELECT count(*) FROM public."+prefix+"blog_posts WHERE id = $1", p.ID).Scan(&postRows))
	assert.Zero(t, postRows, "org A's post must be invisible under org B's tenant scope")

	stillThere, err := store.ByID(ownerCtx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, p.ID, stillThere.ID)
	assert.Equal(t, []uuid.UUID{aTag}, stillThere.TagIDs)
}

// TestPostgres_Post_OtherOrgIsInvisible_WithoutRLS runs the same isolation checks with row level
// security inert. The plain fixture connects as the container superuser, which holds BYPASSRLS,
// so the policies the migration created never filter anything — the same exposure a deployment
// gets from `tenant.rlsEnforce: false`, or from pointing the app at a BYPASSRLS role. What keeps
// org B out here is only the explicit org_id predicate on each statement plus the conflict
// clause's guard.
func TestPostgres_Post_OtherOrgIsInvisible_WithoutRLS(t *testing.T) {
	f := newPgFixture(t)
	ownerCtx := tenant.Into(t.Context(), f.tc)

	var bypass bool
	require.NoError(t, f.sqlDB.QueryRowContext(t.Context(),
		"SELECT rolbypassrls FROM pg_roles WHERE rolname = current_user").Scan(&bypass))
	require.True(t, bypass, "this test proves nothing unless RLS is inert for this connection")

	tagID := seedPgTag(t, f.sqlDB, f.prefix, f.tc)
	p, err := blog.New(f.tc.OrgID, f.tc.ProjectID, f.cat, "Org A Only", "", "body")
	require.NoError(t, err)
	p.SetTags([]uuid.UUID{tagID})
	require.NoError(t, f.store.Save(ownerCtx, p))

	var visible int
	require.NoError(t, f.sqlDB.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+f.prefix+"blog_posts WHERE id = $1", p.ID).Scan(&visible))
	require.Equal(t, 1, visible, "the database must be handing this connection the row unfiltered")

	b := seedPgTenant(t, f.sqlDB, f.prefix)
	bCat := seedPgCategory(t, f.sqlDB, f.prefix, b, b.ProjectID)
	otherCtx := tenant.Into(t.Context(), b)

	_, err = f.store.ByID(otherCtx, p.ID)
	assert.True(t, blog.IsNotFoundError(err), "org B must not see org A's post, got %T: %v", err, err)

	listed, err := f.store.List(otherCtx, f.tc.OrgID, f.tc.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, listed, "naming org A's scope from org B's context must list nothing")

	counts, err := f.store.CountByCategory(otherCtx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, counts, "org B must not count org A's posts")

	tagCounts, err := f.store.CountByTag(otherCtx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, tagCounts, "org B must not count org A's join rows")

	assert.True(t, blog.IsNotFoundError(f.store.Delete(otherCtx, p.ID)),
		"org B must not be able to delete org A's post")

	// NOTE: the handler-shaped attack — org B posts its own scope with a row id it does not
	// own, so nothing but the conflict clause's org predicate can catch it.
	hijack, err := blog.New(b.OrgID, b.ProjectID, bCat, "Hijacked", "", "body")
	require.NoError(t, err)
	hijack.ID = p.ID
	assert.True(t, blog.IsNotFoundError(f.store.Save(otherCtx, hijack)),
		"the conflict guard must refuse this as NotFoundError")

	stillThere, err := f.store.ByID(ownerCtx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Org A Only", stillThere.Title, "org A's row must be untouched")
	assert.Equal(t, f.tc.OrgID, stillThere.OrgID, "org A's row must not have changed hands")
	assert.Equal(t, []uuid.UUID{tagID}, stillThere.TagIDs, "org A's join rows must be untouched")
}
