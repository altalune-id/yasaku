//go:build integration

package tag_test

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

	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/nanoid"
	"altalune.id/yasaku/schema"
)

type pgFixture struct {
	store  tag.Store
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
	store := tag.NewStore(
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

func TestPostgres_Tag_SaveAndByID(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	got, err := f.store.ByID(ctx, tg.ID)
	require.NoError(t, err)
	assert.Equal(t, tg.ID, got.ID)
	assert.Equal(t, "Go Lang", got.Name)
	assert.Equal(t, "go-lang", got.Slug)
	// NOTE: postgres timestamptz is microsecond precision, so a nanosecond-precision
	// time.Now() does not survive the round trip. On macOS the nanoseconds are often
	// already zero, which hides this locally; linux CI fails it.
	assert.True(t, got.CreatedAt.Equal(tg.CreatedAt.Truncate(time.Microsecond)),
		"CreatedAt round-trip: got=%v want=%v", got.CreatedAt, tg.CreatedAt.Truncate(time.Microsecond))
}

func TestPostgres_Tag_NotFound(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	_, err := f.store.ByID(ctx, uuid.New())
	assert.True(t, tag.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestPostgres_Tag_SaveIsUpsert(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	require.NoError(t, tg.Rename("Golang"))
	require.NoError(t, f.store.Save(ctx, tg))

	got, err := f.store.ByID(ctx, tg.ID)
	require.NoError(t, err)
	assert.Equal(t, "Golang", got.Name)
	assert.Equal(t, "go-lang", got.Slug)

	all, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestPostgres_Tag_List_OrdersByCreatedAtThenID(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	base := time.Now().UTC().Truncate(time.Second)
	mk := func(name string, created time.Time) *tag.Tag {
		tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, name, "")
		require.NoError(t, err)
		tg.CreatedAt = created
		tg.UpdatedAt = created
		require.NoError(t, f.store.Save(ctx, tg))
		return tg
	}
	oldest := mk("oldest", base.Add(-2*time.Hour))
	tieA := mk("tie-a", base)
	tieB := mk("tie-b", base)

	got, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	require.Len(t, got, 3)

	hi, lo := tieA, tieB
	if hi.ID.String() < lo.ID.String() {
		hi, lo = lo, hi
	}
	assert.Equal(t, hi.ID, got[0].ID, "ties on created_at must break on id DESC")
	assert.Equal(t, lo.ID, got[1].ID)
	assert.Equal(t, oldest.ID, got[2].ID)
}

func TestPostgres_Tag_DuplicateSlugInOneProject(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	first, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, first))

	dup, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "go lang", "")
	require.NoError(t, err)
	require.Equal(t, first.Slug, dup.Slug)

	err = f.store.Save(ctx, dup)
	assert.True(t, tag.IsAlreadyExistsError(err), "got %T: %v", err, err)
}

func TestPostgres_Tag_SameSlugInTwoProjectsIsAllowed(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	otherProj := seedPgProject(t, f.sqlDB, f.prefix, f.tc.UserID, f.tc.OrgID, "docs")

	a, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, a))

	b, err := tag.New(f.tc.OrgID, otherProj, "Go Lang", "")
	require.NoError(t, err)
	assert.NoError(t, f.store.Save(ctx, b), "uniqueness is per project, not per org")
}

func TestPostgres_Tag_BySlug(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	otherProj := seedPgProject(t, f.sqlDB, f.prefix, f.tc.UserID, f.tc.OrgID, "docs")

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	got, err := f.store.BySlug(ctx, f.tc.OrgID, f.tc.ProjectID, "go-lang")
	require.NoError(t, err)
	assert.Equal(t, tg.ID, got.ID)

	_, err = f.store.BySlug(ctx, f.tc.OrgID, f.tc.ProjectID, "nope")
	assert.True(t, tag.IsNotFoundError(err), "got %T: %v", err, err)

	_, err = f.store.BySlug(ctx, f.tc.OrgID, otherProj, "go-lang")
	assert.True(t, tag.IsNotFoundError(err), "BySlug must be project-scoped, got %T: %v", err, err)
}

func TestPostgres_Tag_ByIDs(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	otherProj := seedPgProject(t, f.sqlDB, f.prefix, f.tc.UserID, f.tc.OrgID, "docs")

	a, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "a", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, a))
	b, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "b", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, b))
	elsewhere, err := tag.New(f.tc.OrgID, otherProj, "c", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, elsewhere))

	unknown := uuid.New()
	got, err := f.store.ByIDs(ctx, f.tc.OrgID, f.tc.ProjectID, []uuid.UUID{a.ID, b.ID, unknown, elsewhere.ID})
	require.NoError(t, err, "unknown ids must be omitted, not an error")
	require.Len(t, got, 2)
	assert.Contains(t, got, a.ID)
	assert.Contains(t, got, b.ID)
	assert.NotContains(t, got, elsewhere.ID, "ByIDs must not cross project boundaries")

	empty, err := f.store.ByIDs(ctx, f.tc.OrgID, f.tc.ProjectID, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestPostgres_Tag_Delete(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "gone", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))
	require.NoError(t, f.store.Delete(ctx, tg.ID))

	_, err = f.store.ByID(ctx, tg.ID)
	assert.True(t, tag.IsNotFoundError(err), "got %T: %v", err, err)
	assert.True(t, tag.IsNotFoundError(f.store.Delete(ctx, tg.ID)), "double delete")
}

func TestPostgres_Tag_Delete_InUse(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "attached", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	// NOTE: raw INSERTs because the post store does not exist yet; this test only needs
	// referencing rows so ON DELETE RESTRICT fires. The join row's org_id must match the
	// post's: the composite FK (post_id, org_id) -> blog_posts(id, org_id) rejects a mismatch.
	now := time.Now().UTC()
	catID, postID := uuid.New(), uuid.New()
	_, err = f.sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"blog_categories (id, org_id, project_id, name, slug, created_at, updated_at) "+
			"VALUES ($1, $2, $3, 'General', 'general', $4, $4)",
		catID, f.tc.OrgID, f.tc.ProjectID, now)
	require.NoError(t, err)
	_, err = f.sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"blog_posts "+
			"(id, org_id, project_id, category_id, title, slug, body_markdown, status, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, 'T', 't', '', 'draft', $5, $5)",
		postID, f.tc.OrgID, f.tc.ProjectID, catID, now)
	require.NoError(t, err)
	_, err = f.sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"blog_post_tags (post_id, tag_id, org_id) VALUES ($1, $2, $3)",
		postID, tg.ID, f.tc.OrgID)
	require.NoError(t, err)

	err = f.store.Delete(ctx, tg.ID)
	require.True(t, tag.IsInUseError(err), "want InUseError, got %T: %v", err, err)

	got, err := f.store.ByID(ctx, tg.ID)
	require.NoError(t, err, "a refused delete must leave the row in place")
	assert.Equal(t, tg.ID, got.ID)
}

// newPgRLSFixture migrates under a BYPASSRLS owner and returns a store bound to a
// NOBYPASSRLS app role, so the row level security policies actually apply. The plain
// fixture above connects as the container superuser, which bypasses RLS outright.
func newPgRLSFixture(t *testing.T) (tag.Store, *sql.DB, string) {
	t.Helper()
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	ownerRole := "yasaku_tagowner_" + suffix
	appRole := "yasaku_tagapp_" + suffix
	prefix := "t" + suffix + "_"

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

	appConn, err := db.Open(t.Context(), db.DBConfig{
		Driver: db.DriverPostgres, DSN: pgtest.DSNWithUser(t, h.DSN, appRole, "pw"), MaxOpenConns: 2,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = appConn.Close() })

	store := tag.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: "public", TablePrefix: prefix},
		db.Pool{W: appConn, R: appConn},
		tenant.NewPgConn(appConn),
	)
	return store, migDB, prefix
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

func TestPostgres_Tag_OtherOrgIsInvisible(t *testing.T) {
	store, migDB, prefix := newPgRLSFixture(t)
	a := seedRLSTenant(t, migDB, prefix)
	b := seedRLSTenant(t, migDB, prefix)

	ownerCtx := tenant.Into(t.Context(), a)
	otherCtx := tenant.Into(t.Context(), b)

	tg, err := tag.New(a.OrgID, a.ProjectID, "Org A Only", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ownerCtx, tg))

	_, err = store.ByID(otherCtx, tg.ID)
	assert.True(t, tag.IsNotFoundError(err), "org B must not see org A's tag, got %T: %v", err, err)

	_, err = store.BySlug(otherCtx, a.OrgID, a.ProjectID, tg.Slug)
	assert.True(t, tag.IsNotFoundError(err), "org B must not read org A's tag by slug, got %T: %v", err, err)

	listed, err := store.List(otherCtx, b.OrgID, b.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, listed)

	byIDs, err := store.ByIDs(otherCtx, a.OrgID, a.ProjectID, []uuid.UUID{tg.ID})
	require.NoError(t, err)
	assert.Empty(t, byIDs, "org B must not read org A's rows even when it names org A's scope")

	assert.True(t, tag.IsNotFoundError(store.Delete(otherCtx, tg.ID)),
		"org B must not be able to delete org A's tag")

	stillThere, err := store.ByID(ownerCtx, tg.ID)
	require.NoError(t, err)
	assert.Equal(t, tg.ID, stillThere.ID)
}

// TestPostgres_Tag_OtherOrgIsInvisible_WithoutRLS runs the same isolation checks with row level
// security inert. The plain fixture connects as the container superuser, which holds BYPASSRLS,
// so the policies the migration created never filter anything — the same exposure a deployment
// gets from `tenant.rlsEnforce: false`, or from pointing the app at a BYPASSRLS role. The test
// proves the database really does hand this connection org A's row, then that every store method
// still refuses it. What keeps org B out here is only the explicit org_id predicate on each
// statement.
func TestPostgres_Tag_OtherOrgIsInvisible_WithoutRLS(t *testing.T) {
	f := newPgFixture(t)
	ownerCtx := tenant.Into(t.Context(), f.tc)

	var bypass bool
	require.NoError(t, f.sqlDB.QueryRowContext(t.Context(),
		"SELECT rolbypassrls FROM pg_roles WHERE rolname = current_user").Scan(&bypass))
	require.True(t, bypass, "this test proves nothing unless RLS is inert for this connection")

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Org A Only", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ownerCtx, tg))

	var visible int
	require.NoError(t, f.sqlDB.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+f.prefix+"blog_tags WHERE id = $1", tg.ID).Scan(&visible))
	require.Equal(t, 1, visible, "the database must be handing this connection the row unfiltered")

	otherUser, otherOrg := seedPgUserAndOrg(t, f.sqlDB, f.prefix)
	otherProj := seedPgProject(t, f.sqlDB, f.prefix, otherUser, otherOrg, "web")
	otherCtx := tenant.Into(t.Context(), tenant.Context{OrgID: otherOrg, ProjectID: otherProj, UserID: otherUser})

	_, err = f.store.ByID(otherCtx, tg.ID)
	assert.True(t, tag.IsNotFoundError(err), "org B must not see org A's tag, got %T: %v", err, err)

	_, err = f.store.BySlug(otherCtx, f.tc.OrgID, f.tc.ProjectID, tg.Slug)
	assert.True(t, tag.IsNotFoundError(err), "org B must not read org A's tag by slug, got %T: %v", err, err)

	listed, err := f.store.List(otherCtx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, listed, "naming org A's scope from org B's context must list nothing")

	byIDs, err := f.store.ByIDs(otherCtx, f.tc.OrgID, f.tc.ProjectID, []uuid.UUID{tg.ID})
	require.NoError(t, err)
	assert.Empty(t, byIDs, "naming org A's scope from org B's context must resolve nothing")

	assert.True(t, tag.IsNotFoundError(f.store.Delete(otherCtx, tg.ID)),
		"org B must not be able to delete org A's tag")

	hijack := *tg
	hijack.Name = "Hijacked"
	assert.True(t, tag.IsNotFoundError(f.store.Save(otherCtx, &hijack)),
		"org B must not be able to upsert onto org A's row")

	stillThere, err := f.store.ByID(ownerCtx, tg.ID)
	require.NoError(t, err)
	assert.Equal(t, tg.ID, stillThere.ID)
	assert.Equal(t, "Org A Only", stillThere.Name, "org A's row must be untouched")
}
