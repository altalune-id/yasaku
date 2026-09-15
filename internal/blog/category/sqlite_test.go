package category_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/schema"
)

// NOTE: a t.TempDir() file DSN through db.Open, never ":memory:" — the pragma must
// reach every pooled connection or ON DELETE RESTRICT silently no-ops.
func newSQLiteStoreForTest(t *testing.T) (category.Store, *sql.DB, string, tenant.Context) {
	t.Helper()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "category_test.db")

	sqlDB, err := db.Open(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	userID, orgID, projID := seedProjectTree(t, sqlDB, prefix)
	tc := tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}

	store := category.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	)
	return store, sqlDB, prefix, tc
}

func seedProjectTree(t *testing.T, sqlDB *sql.DB, prefix string) (userID, orgID, projID uuid.UUID) { //nolint:nonamedreturns // triple
	t.Helper()
	userID, orgID = uuid.New(), uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) "+
			"VALUES (?, ?, '', '', 0, ?, ?)",
		userID.String(), userID.String()+"@example.com", now, now)
	require.NoError(t, err)
	_, err = sqlDB.Exec(
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) "+
			"VALUES (?, ?, 'Org', ?, ?, ?)",
		orgID.String(), orgID.String()[:8], userID.String(), now, now)
	require.NoError(t, err)
	projID = seedProject(t, sqlDB, prefix, userID, orgID, "web")
	return userID, orgID, projID
}

func seedOrg(t *testing.T, sqlDB *sql.DB, prefix string, userID uuid.UUID) uuid.UUID {
	t.Helper()
	orgID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) "+
			"VALUES (?, ?, 'Org', ?, ?, ?)",
		orgID.String(), orgID.String()[:8], userID.String(), now, now)
	require.NoError(t, err)
	return orgID
}

func seedProject(t *testing.T, sqlDB *sql.DB, prefix string, userID, orgID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	projID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?)",
		projID.String(), orgID.String(), slug, slug, userID.String(), now, now)
	require.NoError(t, err)
	return projID
}

func TestSQLite_SaveAndByID(t *testing.T) {
	store, _, _, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	c, err := category.New(tc.OrgID, tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, c))

	got, err := store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, c.ID, got.ID)
	assert.Equal(t, "Release Notes", got.Name)
	assert.Equal(t, "release-notes", got.Slug)
	assert.Equal(t, tc.OrgID, got.OrgID)
	assert.Equal(t, tc.ProjectID, got.ProjectID)
	assert.WithinDuration(t, c.CreatedAt, got.CreatedAt, time.Millisecond)
}

func TestSQLite_ByID_NotFound(t *testing.T) {
	store, _, _, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	_, err := store.ByID(ctx, uuid.New())
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

func TestSQLite_List_OrdersNewestFirst(t *testing.T) {
	store, _, _, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	base := time.Now().UTC().Add(-time.Hour)
	names := []string{"oldest", "middle", "newest"}
	for i, name := range names {
		c, err := category.New(tc.OrgID, tc.ProjectID, name, "")
		require.NoError(t, err)
		c.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		c.UpdatedAt = c.CreatedAt
		require.NoError(t, store.Save(ctx, c))
	}

	got, err := store.List(ctx, tc.OrgID, tc.ProjectID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"newest", "middle", "oldest"},
		[]string{got[0].Name, got[1].Name, got[2].Name})
}

func TestSQLite_Save_DuplicateSlugInSameProject(t *testing.T) {
	store, _, _, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	first, err := category.New(tc.OrgID, tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, first))

	second, err := category.New(tc.OrgID, tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	err = store.Save(ctx, second)
	assert.True(t, category.IsAlreadyExistsError(err), "want AlreadyExistsError, got %T: %v", err, err)
}

func TestSQLite_Save_SameSlugInAnotherProject(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	first, err := category.New(tc.OrgID, tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, first))

	otherProj := seedProject(t, sqlDB, prefix, tc.UserID, tc.OrgID, "docs")
	second, err := category.New(tc.OrgID, otherProj, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, second), "UNIQUE is (project_id, slug), not slug alone")

	inFirst, err := store.List(ctx, tc.OrgID, tc.ProjectID)
	require.NoError(t, err)
	assert.Len(t, inFirst, 1)
	inSecond, err := store.List(ctx, tc.OrgID, otherProj)
	require.NoError(t, err)
	assert.Len(t, inSecond, 1)
}

func TestSQLite_ByIDs(t *testing.T) {
	store, _, _, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	a, err := category.New(tc.OrgID, tc.ProjectID, "Alpha", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, a))
	b, err := category.New(tc.OrgID, tc.ProjectID, "Beta", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, b))

	t.Run("unknown ids are simply absent", func(t *testing.T) {
		missing := uuid.New()
		got, gErr := store.ByIDs(ctx, tc.OrgID, tc.ProjectID, []uuid.UUID{a.ID, missing})
		require.NoError(t, gErr)
		require.Len(t, got, 1)
		assert.Equal(t, "Alpha", got[a.ID].Name)
		assert.NotContains(t, got, missing)
	})

	t.Run("empty input returns an empty map", func(t *testing.T) {
		got, gErr := store.ByIDs(ctx, tc.OrgID, tc.ProjectID, nil)
		require.NoError(t, gErr)
		assert.Empty(t, got)
	})

	t.Run("another project is out of scope", func(t *testing.T) {
		got, gErr := store.ByIDs(ctx, tc.OrgID, uuid.New(), []uuid.UUID{a.ID, b.ID})
		require.NoError(t, gErr)
		assert.Empty(t, got)
	})
}

func TestSQLite_Delete(t *testing.T) {
	store, _, _, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	c, err := category.New(tc.OrgID, tc.ProjectID, "Gone", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, c))
	require.NoError(t, store.Delete(ctx, c.ID))

	_, err = store.ByID(ctx, c.ID)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError after delete, got %T: %v", err, err)

	assert.True(t, category.IsNotFoundError(store.Delete(ctx, uuid.New())),
		"deleting an unknown id must report NotFoundError")
}

func TestSQLite_Delete_InUse(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	c, err := category.New(tc.OrgID, tc.ProjectID, "Has Posts", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, c))

	var categories int
	require.NoError(t, sqlDB.QueryRow(
		"SELECT count(*) FROM "+prefix+"blog_categories WHERE id = ?", c.ID.String()).Scan(&categories))
	require.Equal(t, 1, categories, "the FK target row must exist before the post references it")

	// NOTE: raw INSERT because the post store does not exist yet; this test only
	// needs a referencing row so ON DELETE RESTRICT fires.
	now := sqliteent.SQLiteTime(time.Now())
	_, err = sqlDB.Exec(
		`INSERT INTO `+prefix+`blog_posts
		 (id, org_id, project_id, category_id, title, slug, body_markdown, status, created_at, updated_at)
		 VALUES (?,?,?,?,'T','t','', 'draft', ?, ?)`,
		uuid.NewString(), tc.OrgID.String(), tc.ProjectID.String(), c.ID.String(), now, now)
	require.NoError(t, err)

	err = store.Delete(ctx, c.ID)
	require.True(t, category.IsInUseError(err), "want InUseError, got %T: %v", err, err)

	got, err := store.ByID(ctx, c.ID)
	require.NoError(t, err, "a refused delete must leave the row in place")
	assert.Equal(t, c.ID, got.ID)
}

func TestSQLite_RequiresTenantScope(t *testing.T) {
	store, _, _, tc := newSQLiteStoreForTest(t)

	c, err := category.New(tc.OrgID, tc.ProjectID, "Unscoped", "")
	require.NoError(t, err)

	assert.Error(t, store.Save(context.Background(), c))
	_, err = store.ByID(context.Background(), c.ID)
	assert.Error(t, err)
	_, err = store.List(context.Background(), tc.OrgID, tc.ProjectID)
	assert.Error(t, err)
	assert.Error(t, store.Delete(context.Background(), c.ID))
}

// TestSQLite_Save_CannotUpsertOntoAnotherOrgsRow is the hijack shape: org B calls Save with a
// Category carrying org A's row id. SQLite has no row level security, so the conflict-clause
// org guard in Save is the only thing standing between org B and org A's row.
func TestSQLite_Save_CannotUpsertOntoAnotherOrgsRow(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteStoreForTest(t)
	ownerCtx := tenant.Into(t.Context(), tc)

	c, err := category.New(tc.OrgID, tc.ProjectID, "Org A Only", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ownerCtx, c))

	otherOrg := seedOrg(t, sqlDB, prefix, tc.UserID)
	otherProj := seedProject(t, sqlDB, prefix, tc.UserID, otherOrg, "web")
	otherCtx := tenant.Into(t.Context(), tenant.Context{OrgID: otherOrg, ProjectID: otherProj, UserID: tc.UserID})

	hijack := *c
	hijack.Name = "Hijacked"
	assert.True(t, category.IsNotFoundError(store.Save(otherCtx, &hijack)),
		"org B must not be able to upsert onto org A's row")

	stillThere, err := store.ByID(ownerCtx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "Org A Only", stillThere.Name, "org A's row must be untouched")
	assert.Equal(t, tc.OrgID, stillThere.OrgID)
}

// TestSQLite_Save_UpdatesOwnRow guards the other direction: the org guard must not break a
// legitimate upsert by the owning tenant.
func TestSQLite_Save_UpdatesOwnRow(t *testing.T) {
	store, _, _, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	c, err := category.New(tc.OrgID, tc.ProjectID, "Release Notes", "")
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, c))

	require.NoError(t, c.Rename("Changelog"))
	require.NoError(t, store.Save(ctx, c), "the owning tenant must still be able to update its own row")

	got, err := store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "Changelog", got.Name)
	assert.Equal(t, "release-notes", got.Slug)
}
