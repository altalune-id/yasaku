package blog_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/schema"
)

const prefix = "yasaku_"

func newBlogStoreForTest(t *testing.T) (blog.Store, *sql.DB, tenant.Context, uuid.UUID) {
	t.Helper()

	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	// NOTE: a temp-file DSN, not :memory:, or pooled connections do not share the database and cascades never fire.
	cfg.DB.DSN = filepath.Join(t.TempDir(), "blog.db")

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sqlDB, err := db.Open(t.Context(), cfg.DB, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	var fk int
	require.NoError(t, sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&fk))
	require.Equal(t, 1, fk, "foreign keys must be on or the join table constraints cannot be exercised")

	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	require.Equal(t, prefix, cfg.DB.TablePrefix)

	tc := seedTenant(t, sqlDB)
	return blog.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	), sqlDB, tc, seedCategory(t, sqlDB, tc)
}

func seedTenant(t *testing.T, sqlDB *sql.DB) tenant.Context {
	t.Helper()
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
	now := sqliteent.SQLiteTime(time.Now())

	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
		userID.String(), userID.String()+"@x.com", now, now)
	require.NoError(t, err)

	_, err = sqlDB.Exec(
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Org', ?, ?, ?)",
		orgID.String(), orgID.String()[:8], userID.String(), now, now)
	require.NoError(t, err)

	_, err = sqlDB.Exec(
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Web', ?, ?, ?)",
		projID.String(), orgID.String(), projID.String()[:8], userID.String(), now, now)
	require.NoError(t, err)

	return tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}
}

func seedProject(t *testing.T, sqlDB *sql.DB, tc tenant.Context) uuid.UUID {
	t.Helper()
	projID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Docs', ?, ?, ?)",
		projID.String(), tc.OrgID.String(), projID.String()[:8], tc.UserID.String(), now, now)
	require.NoError(t, err)
	return projID
}

func seedCategory(t *testing.T, sqlDB *sql.DB, tc tenant.Context) uuid.UUID {
	t.Helper()
	return seedCategoryIn(t, sqlDB, tc, tc.ProjectID)
}

func seedCategoryIn(t *testing.T, sqlDB *sql.DB, tc tenant.Context, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"blog_categories (id, org_id, project_id, name, slug, created_at, updated_at) VALUES (?,?,?,?,?,?,?)",
		id.String(), tc.OrgID.String(), projectID.String(), "Cat "+id.String()[:8], id.String()[:8], now, now)
	require.NoError(t, err)
	return id
}

func seedTag(t *testing.T, sqlDB *sql.DB, tc tenant.Context) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"blog_tags (id, org_id, project_id, name, slug, created_at, updated_at) VALUES (?,?,?,?,?,?,?)",
		id.String(), tc.OrgID.String(), tc.ProjectID.String(), "Tag "+id.String()[:8], id.String()[:8], now, now)
	require.NoError(t, err)
	return id
}

func TestSQLiteStore_SaveAndByID(t *testing.T) {
	store, _, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Hello World", "", "# body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, p, 0))

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, "Hello World", got.Title)
	assert.Equal(t, "hello-world", got.Slug)
	assert.Equal(t, "# body", got.BodyMarkdown)
	assert.Equal(t, blog.StatusDraft, got.Status)
	assert.Equal(t, cat, got.CategoryID)
	assert.Nil(t, got.FirstPublishedAt)
	assert.Empty(t, got.TagIDs)
	assert.True(t, got.CreatedAt.Equal(p.CreatedAt), "CreatedAt round-trip: got=%v want=%v", got.CreatedAt, p.CreatedAt)
	assert.True(t, got.UpdatedAt.Equal(p.UpdatedAt), "UpdatedAt round-trip: got=%v want=%v", got.UpdatedAt, p.UpdatedAt)
}

func TestSQLiteStore_ByID_NotFound(t *testing.T) {
	store, _, tc, _ := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	_, err := store.ByID(ctx, uuid.New())
	assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestSQLiteStore_SaveRoundTripsPublication(t *testing.T) {
	store, _, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Published", "", "body")
	require.NoError(t, err)
	p.Publish()
	require.NoError(t, store.Save(ctx, p, 0))

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, blog.StatusPublished, got.Status)
	require.NotNil(t, got.FirstPublishedAt)
	assert.True(t, got.FirstPublishedAt.Equal(*p.FirstPublishedAt),
		"FirstPublishedAt round-trip: got=%v want=%v", got.FirstPublishedAt, p.FirstPublishedAt)

	first := *p.FirstPublishedAt
	p.Unpublish()
	require.NoError(t, store.Save(ctx, p, 0))

	got, err = store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, blog.StatusDraft, got.Status)
	require.NotNil(t, got.FirstPublishedAt, "unpublishing must retain the first publication")
	assert.True(t, got.FirstPublishedAt.Equal(first))
}

func TestSQLiteStore_SaveReplacesTagSetWithoutOrphans(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "First", "", "body")
	require.NoError(t, err)
	a, b, c := seedTag(t, sqlDB, tc), seedTag(t, sqlDB, tc), seedTag(t, sqlDB, tc)

	p.SetTags([]uuid.UUID{a, b})
	require.NoError(t, store.Save(ctx, p, 0))

	p.SetTags([]uuid.UUID{b, c})
	require.NoError(t, store.Save(ctx, p, 0))

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{b, c}, got.TagIDs)

	var rows int
	require.NoError(t, sqlDB.QueryRow(
		"SELECT count(*) FROM yasaku_blog_post_tags WHERE post_id = ?", p.ID.String()).Scan(&rows))
	require.Equal(t, 2, rows, "replacing the tag set must leave no orphan join rows")
}

func TestSQLiteStore_SaveClearsTagSet(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "First", "", "body")
	require.NoError(t, err)
	p.SetTags([]uuid.UUID{seedTag(t, sqlDB, tc)})
	require.NoError(t, store.Save(ctx, p, 0))

	p.SetTags(nil)
	require.NoError(t, store.Save(ctx, p, 0))

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Empty(t, got.TagIDs)

	var rows int
	require.NoError(t, sqlDB.QueryRow(
		"SELECT count(*) FROM "+prefix+"blog_post_tags WHERE post_id = ?", p.ID.String()).Scan(&rows))
	assert.Zero(t, rows)
}

func TestSQLiteStore_SaveWithUnknownTagIsRefusedWhole(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "First", "", "body")
	require.NoError(t, err)
	good := seedTag(t, sqlDB, tc)
	p.SetTags([]uuid.UUID{good})
	require.NoError(t, store.Save(ctx, p, 0))

	p.SetTags([]uuid.UUID{good, uuid.New()})
	require.Error(t, store.Save(ctx, p, 0), "an unknown tag id must fail the foreign key")

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{good}, got.TagIDs,
		"a failed Save must roll back the tag replacement, not half-apply it")
}

func TestSQLiteStore_DuplicateSlugInOneProject(t *testing.T) {
	store, _, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	first, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Hello World", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, first, 0))

	dup, err := blog.New(tc.OrgID, tc.ProjectID, cat, "hello   world", "", "body")
	require.NoError(t, err)
	require.Equal(t, first.Slug, dup.Slug)

	err = store.Save(ctx, dup, 0)
	assert.True(t, blog.IsAlreadyExistsError(err), "got %T: %v", err, err)
}

func TestSQLiteStore_SameSlugInTwoProjectsIsAllowed(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)
	otherProj := seedProject(t, sqlDB, tc)
	otherCat := seedCategoryIn(t, sqlDB, tc, otherProj)

	a, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Hello World", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, a, 0))

	b, err := blog.New(tc.OrgID, otherProj, otherCat, "Hello World", "", "body")
	require.NoError(t, err)
	require.Equal(t, a.Slug, b.Slug)
	assert.NoError(t, store.Save(ctx, b, 0), "uniqueness is per project, not per org")
}

func TestSQLiteStore_List_OrdersByCreatedAtThenID(t *testing.T) {
	store, _, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	base := time.Now().UTC().Truncate(time.Second)
	mk := func(title string, created time.Time) *blog.Post {
		p, err := blog.New(tc.OrgID, tc.ProjectID, cat, title, "", "body")
		require.NoError(t, err)
		p.CreatedAt = created
		p.UpdatedAt = created
		require.NoError(t, store.Save(ctx, p, 0))
		return p
	}
	oldest := mk("oldest", base.Add(-2*time.Hour))
	tieA := mk("tie-a", base)
	tieB := mk("tie-b", base)

	got, err := store.List(ctx, tc.OrgID, tc.ProjectID, blog.ListOpts{})
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

func TestSQLiteStore_List_FiltersByStatusAndCategory(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)
	otherCat := seedCategory(t, sqlDB, tc)

	draft, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Draft", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, draft, 0))

	published, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Published", "", "body")
	require.NoError(t, err)
	published.Publish()
	require.NoError(t, store.Save(ctx, published, 0))

	elsewhere, err := blog.New(tc.OrgID, tc.ProjectID, otherCat, "Elsewhere", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, elsewhere, 0))

	all, err := store.List(ctx, tc.OrgID, tc.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 3)

	wantPublished := blog.StatusPublished
	byStatus, err := store.List(ctx, tc.OrgID, tc.ProjectID, blog.ListOpts{Status: &wantPublished})
	require.NoError(t, err)
	require.Len(t, byStatus, 1)
	assert.Equal(t, published.ID, byStatus[0].ID)

	byCategory, err := store.List(ctx, tc.OrgID, tc.ProjectID, blog.ListOpts{CategoryID: &otherCat})
	require.NoError(t, err)
	require.Len(t, byCategory, 1)
	assert.Equal(t, elsewhere.ID, byCategory[0].ID)

	both, err := store.List(ctx, tc.OrgID, tc.ProjectID, blog.ListOpts{Status: &wantPublished, CategoryID: &otherCat})
	require.NoError(t, err)
	assert.Empty(t, both, "both filters must apply together")
}

func TestSQLiteStore_List_AttachesTagsWithoutMultiplyingRows(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)
	a, b := seedTag(t, sqlDB, tc), seedTag(t, sqlDB, tc)

	tagged, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Tagged", "", "body")
	require.NoError(t, err)
	tagged.SetTags([]uuid.UUID{a, b})
	require.NoError(t, store.Save(ctx, tagged, 0))

	bare, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Bare", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, bare, 0))

	got, err := store.List(ctx, tc.OrgID, tc.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	require.Len(t, got, 2, "two tags on one post must not yield two rows for it")

	byID := map[uuid.UUID][]uuid.UUID{}
	for _, p := range got {
		byID[p.ID] = p.TagIDs
	}
	assert.ElementsMatch(t, []uuid.UUID{a, b}, byID[tagged.ID])
	assert.Empty(t, byID[bare.ID])
}

func TestSQLiteStore_List_ScopesToTheProject(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)
	otherProj := seedProject(t, sqlDB, tc)
	otherCat := seedCategoryIn(t, sqlDB, tc, otherProj)

	mine, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Mine", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, mine, 0))

	theirs, err := blog.New(tc.OrgID, otherProj, otherCat, "Theirs", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, theirs, 0))

	got, err := store.List(ctx, tc.OrgID, tc.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, mine.ID, got[0].ID)

	foreign, err := store.List(ctx, uuid.New(), tc.ProjectID, blog.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, foreign, "another org's scope must list nothing")
}

func TestSQLiteStore_Delete(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Gone", "", "body")
	require.NoError(t, err)
	p.SetTags([]uuid.UUID{seedTag(t, sqlDB, tc), seedTag(t, sqlDB, tc)})
	require.NoError(t, store.Save(ctx, p, 0))

	require.NoError(t, store.Delete(ctx, p.ID, 0))

	_, err = store.ByID(ctx, p.ID)
	assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
	assert.True(t, blog.IsNotFoundError(store.Delete(ctx, p.ID, 0)), "double delete")

	var rows int
	require.NoError(t, sqlDB.QueryRow(
		"SELECT count(*) FROM "+prefix+"blog_post_tags WHERE post_id = ?", p.ID.String()).Scan(&rows))
	assert.Zero(t, rows, "deleting a post must take its join rows with it")
}

func TestSQLiteStore_CountByCategoryIsOneQueryPerProject(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)
	other := seedCategory(t, sqlDB, tc)

	for range 3 {
		p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "T"+uuid.NewString(), "", "b")
		require.NoError(t, err)
		require.NoError(t, store.Save(ctx, p, 0))
	}

	counts, err := store.CountByCategory(ctx, tc.OrgID, tc.ProjectID)
	require.NoError(t, err)
	require.Equal(t, 3, counts[cat])
	_, present := counts[other]
	require.False(t, present, "a category with no posts is absent, not zero-valued")
}

func TestSQLiteStore_CountByCategory_ScopesToTheProject(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)
	otherProj := seedProject(t, sqlDB, tc)
	otherCat := seedCategoryIn(t, sqlDB, tc, otherProj)

	mine, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Mine", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, mine, 0))

	theirs, err := blog.New(tc.OrgID, otherProj, otherCat, "Theirs", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, theirs, 0))

	counts, err := store.CountByCategory(ctx, tc.OrgID, tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID]int{cat: 1}, counts)

	foreign, err := store.CountByCategory(ctx, uuid.New(), tc.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, foreign, "another org's scope must count nothing")
}

func TestSQLiteStore_CountByTag(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)
	a, b, unused := seedTag(t, sqlDB, tc), seedTag(t, sqlDB, tc), seedTag(t, sqlDB, tc)

	first, err := blog.New(tc.OrgID, tc.ProjectID, cat, "First", "", "body")
	require.NoError(t, err)
	first.SetTags([]uuid.UUID{a, b})
	require.NoError(t, store.Save(ctx, first, 0))

	second, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Second", "", "body")
	require.NoError(t, err)
	second.SetTags([]uuid.UUID{a})
	require.NoError(t, store.Save(ctx, second, 0))

	counts, err := store.CountByTag(ctx, tc.OrgID, tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, 2, counts[a])
	assert.Equal(t, 1, counts[b])
	_, present := counts[unused]
	assert.False(t, present, "a tag with no posts is absent, not zero-valued")

	foreign, err := store.CountByTag(ctx, uuid.New(), tc.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, foreign, "another org's scope must count nothing")
}

func TestSQLiteStore_TenantMissing(t *testing.T) {
	store, _, tc, cat := newBlogStoreForTest(t)
	bare := context.Background()

	p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "X", "", "body")
	require.NoError(t, err)

	assert.True(t, tenant.IsMissingError(store.Save(bare, p, 0)))

	_, err = store.ByID(bare, p.ID)
	assert.True(t, tenant.IsMissingError(err))

	_, err = store.List(bare, tc.OrgID, tc.ProjectID, blog.ListOpts{})
	assert.True(t, tenant.IsMissingError(err))

	_, err = store.CountByCategory(bare, tc.OrgID, tc.ProjectID)
	assert.True(t, tenant.IsMissingError(err))

	_, err = store.CountByTag(bare, tc.OrgID, tc.ProjectID)
	assert.True(t, tenant.IsMissingError(err))

	assert.True(t, tenant.IsMissingError(store.Delete(bare, p.ID, 0)))
}

// TestSQLiteStore_Save_RefusesCrossTenantUpsert is the regression detector for the conflict clause's org guard, which only SQLite can prove.
func TestSQLiteStore_Save_RefusesCrossTenantUpsert(t *testing.T) {
	t.Run("updates the caller's own row", func(t *testing.T) {
		store, _, tc, cat := newBlogStoreForTest(t)
		ctx := tenant.Into(t.Context(), tc)

		p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Original", "", "body")
		require.NoError(t, err)
		require.NoError(t, store.Save(ctx, p, 0))

		require.NoError(t, p.Update("Renamed", p.Slug, "body", cat))
		require.NoError(t, store.Save(ctx, p, 0), "the guard must not refuse the caller's own row")

		got, err := store.ByID(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", got.Title, "a same-org upsert must still take effect")
	})

	t.Run("verbatim copy of another org's row", func(t *testing.T) {
		store, sqlDB, tc, cat := newBlogStoreForTest(t)
		ownerCtx := tenant.Into(t.Context(), tc)
		b := seedTenant(t, sqlDB)
		otherCtx := tenant.Into(t.Context(), b)

		victim, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Org A Only", "", "body")
		require.NoError(t, err)
		require.NoError(t, store.Save(ownerCtx, victim, 0))

		hijack := *victim
		hijack.Title = "Hijacked"
		require.Error(t, store.Save(otherCtx, &hijack, 0), "org B must not be able to upsert onto org A's row")

		got, err := store.ByID(ownerCtx, victim.ID)
		require.NoError(t, err)
		assert.Equal(t, "Org A Only", got.Title, "org A's row must be untouched")
	})

	t.Run("reskinned with the caller's own scope but another org's row id", func(t *testing.T) {
		store, sqlDB, tc, cat := newBlogStoreForTest(t)
		ownerCtx := tenant.Into(t.Context(), tc)
		b := seedTenant(t, sqlDB)
		bCat := seedCategory(t, sqlDB, b)
		otherCtx := tenant.Into(t.Context(), b)

		victim, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Org A Only", "", "body")
		require.NoError(t, err)
		require.NoError(t, store.Save(ownerCtx, victim, 0))

		// NOTE: org B posts its own scope with a row id it does not own, so only the conflict clause's org predicate can catch it.
		hijack, err := blog.New(b.OrgID, b.ProjectID, bCat, "Hijacked", "", "body")
		require.NoError(t, err)
		hijack.ID = victim.ID

		err = store.Save(otherCtx, hijack, 0)
		assert.True(t, blog.IsNotFoundError(err),
			"the conflict guard must refuse this as NotFoundError, got %T: %v", err, err)

		got, err := store.ByID(ownerCtx, victim.ID)
		require.NoError(t, err)
		assert.Equal(t, "Org A Only", got.Title, "org A's row must be untouched")
		assert.Equal(t, tc.OrgID, got.OrgID, "org A's row must not have changed hands")

		listed, err := store.List(otherCtx, b.OrgID, b.ProjectID, blog.ListOpts{})
		require.NoError(t, err)
		assert.Empty(t, listed, "the refused upsert must not have landed in org B either")
	})

	t.Run("a refused upsert leaves the victim's tags alone", func(t *testing.T) {
		store, sqlDB, tc, cat := newBlogStoreForTest(t)
		ownerCtx := tenant.Into(t.Context(), tc)
		b := seedTenant(t, sqlDB)
		bCat := seedCategory(t, sqlDB, b)
		otherCtx := tenant.Into(t.Context(), b)

		victim, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Org A Only", "", "body")
		require.NoError(t, err)
		victim.SetTags([]uuid.UUID{seedTag(t, sqlDB, tc)})
		require.NoError(t, store.Save(ownerCtx, victim, 0))

		hijack, err := blog.New(b.OrgID, b.ProjectID, bCat, "Hijacked", "", "body")
		require.NoError(t, err)
		hijack.ID = victim.ID
		require.Error(t, store.Save(otherCtx, hijack, 0))

		got, err := store.ByID(ownerCtx, victim.ID)
		require.NoError(t, err)
		assert.Equal(t, victim.TagIDs, got.TagIDs, "the refused upsert must not have cleared org A's join rows")
	})
}

// TestSQLiteStore_StaleVersionMessageNamesTheStoredVersion pins the operator-facing string: the store must name the version it refused, not a zero it never looked up.
func TestSQLiteStore_StaleVersionMessageNamesTheStoredVersion(t *testing.T) {
	cases := []struct {
		name string
		call func(t *testing.T, store blog.Store, ctx context.Context, p *blog.Post) error
	}{
		{
			name: "save",
			call: func(t *testing.T, store blog.Store, ctx context.Context, p *blog.Post) error {
				t.Helper()
				return store.Save(ctx, p, 1)
			},
		},
		{
			name: "delete",
			call: func(t *testing.T, store blog.Store, ctx context.Context, p *blog.Post) error {
				t.Helper()
				return store.Delete(ctx, p.ID, 1)
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			store, _, tc, cat := newBlogStoreForTest(t)
			ctx := tenant.Into(t.Context(), tc)

			p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Original", "", "body")
			require.NoError(t, err)
			require.NoError(t, store.Save(ctx, p, 0))
			require.NoError(t, p.Update("Second", p.Slug, "body", cat))
			require.NoError(t, store.Save(ctx, p, 1), "the first conditional write moves the stored version to 2")

			err = tt.call(t, store, ctx, p)
			require.True(t, blog.IsStaleVersionError(err), "got %T: %v", err, err)
			assert.Equal(t, "blog: stale version, have 2 want 1", err.Error())
		})
	}
}

// TestSQLiteStore_CrossTenantConditionalWriteIsNotFound proves the refusal read stays org-scoped, so a stale-version answer never confirms another org's row.
func TestSQLiteStore_CrossTenantConditionalWriteIsNotFound(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ownerCtx := tenant.Into(t.Context(), tc)
	b := seedTenant(t, sqlDB)
	otherCtx := tenant.Into(t.Context(), b)

	victim, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Org A Only", "", "body")
	require.NoError(t, err)
	require.NoError(t, store.Save(ownerCtx, victim, 0))

	hijack := *victim
	hijack.Title = "Hijacked"
	err = store.Save(otherCtx, &hijack, 1)
	assert.True(t, blog.IsNotFoundError(err), "got %T: %v", err, err)
	assert.False(t, blog.IsStaleVersionError(err), "a stale-version answer would confirm org A's row exists")

	assert.True(t, blog.IsNotFoundError(store.Delete(otherCtx, victim.ID, 1)))
}

// SECURITY: endTx never rolls back an ambient transaction, so a refused conditional delete must not leave an empty tag set on a live post.
func TestSQLiteStore_RefusedConditionalDeleteLeavesTagLinksIntact(t *testing.T) {
	store, sqlDB, tc, cat := newBlogStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)
	tag := seedTag(t, sqlDB, tc)

	p, err := blog.New(tc.OrgID, tc.ProjectID, cat, "Keep", "", "body")
	require.NoError(t, err)
	p.SetTags([]uuid.UUID{tag})
	require.NoError(t, store.Save(ctx, p, 0))

	var delErr error
	require.NoError(t, db.RunInTx(ctx, db.Pool{W: sqlDB, R: sqlDB}, func(txCtx context.Context) error {
		delErr = store.Delete(txCtx, p.ID, p.Version+1)
		return nil
	}), "a caller that treats a stale version as routine commits the ambient transaction")
	require.True(t, blog.IsStaleVersionError(delErr), "got %T: %v", delErr, delErr)

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{tag}, got.TagIDs, "a refused delete must leave the tag set alone")
}
