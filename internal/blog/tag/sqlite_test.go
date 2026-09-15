package tag_test

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

	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/schema"
)

type sqliteFixture struct {
	store    tag.Store
	sqlDB    *sql.DB
	prefix   string
	tc       tenant.Context
	otherPrj uuid.UUID
}

func newSQLiteFixture(t *testing.T) sqliteFixture {
	t.Helper()

	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	// NOTE: a temp-file DSN, not :memory: — every pooled connection must see the same
	// database with foreign keys on, or ON DELETE RESTRICT never fires.
	cfg.DB.DSN = filepath.Join(t.TempDir(), "tag.db")

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sqlDB, err := db.Open(t.Context(), cfg.DB, log)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	var fk int
	require.NoError(t, sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&fk))
	require.Equal(t, 1, fk, "foreign keys must be on or the InUse case cannot be exercised")

	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	userID, orgID, projID, otherPrj := seedProjectTree(t, sqlDB, prefix)

	store := tag.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	)
	return sqliteFixture{
		store:    store,
		sqlDB:    sqlDB,
		prefix:   prefix,
		tc:       tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID},
		otherPrj: otherPrj,
	}
}

func seedProjectTree(t *testing.T, sqlDB *sql.DB, prefix string) (userID, orgID, projID, otherProjID uuid.UUID) { //nolint:nonamedreturns // four ids
	t.Helper()
	userID, orgID, projID, otherProjID = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := sqliteent.SQLiteTime(time.Now())

	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
		userID.String(), userID.String()+"@x.com", now, now)
	require.NoError(t, err)

	_, err = sqlDB.Exec(
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Org', ?, ?, ?)",
		orgID.String(), orgID.String()[:8], userID.String(), now, now)
	require.NoError(t, err)

	for _, p := range []struct {
		id   uuid.UUID
		slug string
	}{{projID, "web"}, {otherProjID, "docs"}} {
		_, err = sqlDB.Exec(
			"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			p.id.String(), orgID.String(), p.slug, p.slug, userID.String(), now, now)
		require.NoError(t, err)
	}
	return userID, orgID, projID, otherProjID
}

func TestSQLiteStore_SaveAndByID(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	got, err := f.store.ByID(ctx, tg.ID)
	require.NoError(t, err)
	assert.Equal(t, "Go Lang", got.Name)
	assert.Equal(t, "go-lang", got.Slug)
	assert.Equal(t, f.tc.OrgID, got.OrgID)
	assert.Equal(t, f.tc.ProjectID, got.ProjectID)
	assert.True(t, got.CreatedAt.Equal(tg.CreatedAt), "CreatedAt round-trip: got=%v want=%v", got.CreatedAt, tg.CreatedAt)
	assert.True(t, got.UpdatedAt.Equal(tg.UpdatedAt), "UpdatedAt round-trip: got=%v want=%v", got.UpdatedAt, tg.UpdatedAt)
}

func TestSQLiteStore_ByID_NotFound(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	_, err := f.store.ByID(ctx, uuid.New())
	assert.True(t, tag.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestSQLiteStore_SaveIsUpsert(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	require.NoError(t, tg.Rename("Golang"))
	require.NoError(t, f.store.Save(ctx, tg))

	got, err := f.store.ByID(ctx, tg.ID)
	require.NoError(t, err)
	assert.Equal(t, "Golang", got.Name)
	assert.Equal(t, "go-lang", got.Slug, "a rename must not move the slug")

	all, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Len(t, all, 1, "the upsert inserted a second row")
}

func TestSQLiteStore_List_OrdersByCreatedAtThenID(t *testing.T) {
	f := newSQLiteFixture(t)
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

func TestSQLiteStore_List_ScopesToTheProject(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	mine, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "mine", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, mine))

	theirs, err := tag.New(f.tc.OrgID, f.otherPrj, "theirs", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, theirs))

	got, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, mine.ID, got[0].ID)

	other, err := f.store.List(ctx, f.tc.OrgID, f.otherPrj)
	require.NoError(t, err)
	require.Len(t, other, 1)
	assert.Equal(t, theirs.ID, other[0].ID)

	foreign, err := f.store.List(ctx, uuid.New(), f.tc.ProjectID)
	require.NoError(t, err)
	assert.Empty(t, foreign, "another org's scope must list nothing")
}

func TestSQLiteStore_DuplicateSlugInOneProject(t *testing.T) {
	f := newSQLiteFixture(t)
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

func TestSQLiteStore_SameSlugInTwoProjectsIsAllowed(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	a, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, a))

	b, err := tag.New(f.tc.OrgID, f.otherPrj, "Go Lang", "")
	require.NoError(t, err)
	require.Equal(t, a.Slug, b.Slug)
	assert.NoError(t, f.store.Save(ctx, b), "uniqueness is per project, not per org")
}

func TestSQLiteStore_BySlug(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Go Lang", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	got, err := f.store.BySlug(ctx, f.tc.OrgID, f.tc.ProjectID, "go-lang")
	require.NoError(t, err)
	assert.Equal(t, tg.ID, got.ID)

	_, err = f.store.BySlug(ctx, f.tc.OrgID, f.tc.ProjectID, "nope")
	assert.True(t, tag.IsNotFoundError(err), "a miss must be NotFoundError, got %T: %v", err, err)

	_, err = f.store.BySlug(ctx, f.tc.OrgID, f.otherPrj, "go-lang")
	assert.True(t, tag.IsNotFoundError(err), "BySlug must be project-scoped, got %T: %v", err, err)
}

func TestSQLiteStore_ByIDs(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	a, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "a", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, a))
	b, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "b", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, b))
	elsewhere, err := tag.New(f.tc.OrgID, f.otherPrj, "c", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, elsewhere))

	unknown := uuid.New()
	got, err := f.store.ByIDs(ctx, f.tc.OrgID, f.tc.ProjectID, []uuid.UUID{a.ID, b.ID, unknown, elsewhere.ID})
	require.NoError(t, err, "unknown ids must be omitted, not an error")
	require.Len(t, got, 2)
	assert.Contains(t, got, a.ID)
	assert.Contains(t, got, b.ID)
	assert.NotContains(t, got, unknown)
	assert.NotContains(t, got, elsewhere.ID, "ByIDs must not cross project boundaries")

	empty, err := f.store.ByIDs(ctx, f.tc.OrgID, f.tc.ProjectID, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestSQLiteStore_Delete(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "gone", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	require.NoError(t, f.store.Delete(ctx, tg.ID))

	_, err = f.store.ByID(ctx, tg.ID)
	assert.True(t, tag.IsNotFoundError(err), "got %T: %v", err, err)

	err = f.store.Delete(ctx, tg.ID)
	assert.True(t, tag.IsNotFoundError(err), "double delete: got %T: %v", err, err)
}

func TestSQLiteStore_Delete_InUse(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "attached", "")
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, tg))

	// NOTE: blog.Store cannot write these two tables yet (Task 7), so the referencing
	// rows go in with raw SQL. The join row's org_id must match the post's: the composite
	// FK (post_id, org_id) -> blog_posts(id, org_id) rejects a mismatch.
	now := sqliteent.SQLiteTime(time.Now())
	catID := uuid.New()
	_, err = f.sqlDB.Exec(
		"INSERT INTO "+f.prefix+"blog_categories (id, org_id, project_id, name, slug, created_at, updated_at) VALUES (?,?,?,'General','general',?,?)",
		catID.String(), f.tc.OrgID.String(), f.tc.ProjectID.String(), now, now)
	require.NoError(t, err)

	postID := uuid.New()
	_, err = f.sqlDB.Exec(
		"INSERT INTO "+f.prefix+"blog_posts (id, org_id, project_id, category_id, title, slug, body_markdown, status, created_at, updated_at) "+
			"VALUES (?,?,?,?,'T','t','','draft',?,?)",
		postID.String(), f.tc.OrgID.String(), f.tc.ProjectID.String(), catID.String(), now, now)
	require.NoError(t, err)

	_, err = f.sqlDB.Exec(
		"INSERT INTO "+f.prefix+"blog_post_tags (post_id, tag_id, org_id) VALUES (?,?,?)",
		postID.String(), tg.ID.String(), f.tc.OrgID.String())
	require.NoError(t, err)

	err = f.store.Delete(ctx, tg.ID)
	assert.True(t, tag.IsInUseError(err), "an attached tag must refuse deletion, got %T: %v", err, err)

	_, err = f.store.ByID(ctx, tg.ID)
	assert.NoError(t, err, "the refused delete must leave the row in place")
}

func TestSQLiteStore_TenantMissing(t *testing.T) {
	f := newSQLiteFixture(t)
	bare := context.Background()

	tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "x", "")
	require.NoError(t, err)

	assert.True(t, tenant.IsMissingError(f.store.Save(bare, tg)))

	_, err = f.store.ByID(bare, tg.ID)
	assert.True(t, tenant.IsMissingError(err))

	_, err = f.store.BySlug(bare, f.tc.OrgID, f.tc.ProjectID, "x")
	assert.True(t, tenant.IsMissingError(err))

	_, err = f.store.List(bare, f.tc.OrgID, f.tc.ProjectID)
	assert.True(t, tenant.IsMissingError(err))

	_, err = f.store.ByIDs(bare, f.tc.OrgID, f.tc.ProjectID, []uuid.UUID{tg.ID})
	assert.True(t, tenant.IsMissingError(err))

	assert.True(t, tenant.IsMissingError(f.store.Delete(bare, tg.ID)))
}

// seedSecondTenant adds another org, user and project to the same database so a hijack
// attempt has a genuinely foreign tenant to come from.
func seedSecondTenant(t *testing.T, sqlDB *sql.DB, prefix string) tenant.Context {
	t.Helper()
	userID, orgID, projID, _ := seedProjectTree(t, sqlDB, prefix)
	return tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}
}

// TestSQLiteStore_Save_RefusesCrossTenantUpsert is the regression detector for the conflict
// clause's org guard. It has to live on SQLite (or an inert-RLS Postgres fixture): under
// enforced RLS the database refuses these writes on its own, so the same test would pass with
// the guard deleted. SQLite has no RLS at all, so here the guard is the only thing saying no.
func TestSQLiteStore_Save_RefusesCrossTenantUpsert(t *testing.T) {
	t.Run("updates the caller's own row", func(t *testing.T) {
		f := newSQLiteFixture(t)
		ctx := tenant.Into(t.Context(), f.tc)

		tg, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Original", "")
		require.NoError(t, err)
		require.NoError(t, f.store.Save(ctx, tg))

		require.NoError(t, tg.Rename("Renamed"))
		require.NoError(t, f.store.Save(ctx, tg), "the guard must not refuse the caller's own row")

		got, err := f.store.ByID(ctx, tg.ID)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", got.Name, "a same-org upsert must still take effect")
	})

	t.Run("verbatim copy of another org's row", func(t *testing.T) {
		f := newSQLiteFixture(t)
		ownerCtx := tenant.Into(t.Context(), f.tc)
		b := seedSecondTenant(t, f.sqlDB, f.prefix)
		otherCtx := tenant.Into(t.Context(), b)

		victim, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Org A Only", "")
		require.NoError(t, err)
		require.NoError(t, f.store.Save(ownerCtx, victim))

		hijack := *victim
		hijack.Name = "Hijacked"
		err = f.store.Save(otherCtx, &hijack)
		require.Error(t, err, "org B must not be able to upsert onto org A's row")

		got, err := f.store.ByID(ownerCtx, victim.ID)
		require.NoError(t, err)
		assert.Equal(t, "Org A Only", got.Name, "org A's row must be untouched")
	})

	t.Run("reskinned with the caller's own scope but another org's row id", func(t *testing.T) {
		f := newSQLiteFixture(t)
		ownerCtx := tenant.Into(t.Context(), f.tc)
		b := seedSecondTenant(t, f.sqlDB, f.prefix)
		otherCtx := tenant.Into(t.Context(), b)

		victim, err := tag.New(f.tc.OrgID, f.tc.ProjectID, "Org A Only", "")
		require.NoError(t, err)
		require.NoError(t, f.store.Save(ownerCtx, victim))

		// NOTE: the handler-shaped attack — org B posts its own scope with a row id it does
		// not own, so nothing but the conflict clause's org predicate can catch it.
		hijack, err := tag.New(b.OrgID, b.ProjectID, "Hijacked", "")
		require.NoError(t, err)
		hijack.ID = victim.ID

		err = f.store.Save(otherCtx, hijack)
		assert.True(t, tag.IsNotFoundError(err),
			"the conflict guard must refuse this as NotFoundError, got %T: %v", err, err)

		got, err := f.store.ByID(ownerCtx, victim.ID)
		require.NoError(t, err)
		assert.Equal(t, "Org A Only", got.Name, "org A's row must be untouched")
		assert.Equal(t, f.tc.OrgID, got.OrgID, "org A's row must not have changed hands")

		listed, err := f.store.List(otherCtx, b.OrgID, b.ProjectID)
		require.NoError(t, err)
		assert.Empty(t, listed, "the refused upsert must not have landed in org B either")
	})
}
