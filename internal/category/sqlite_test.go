package category_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/schema"
)

// NOTE: a t.TempDir() file DSN through db.Open, never ":memory:" — the foreign_keys pragma
// must reach every pooled connection or ON DELETE RESTRICT silently no-ops.
func newSQLiteFixture(t *testing.T) (category.Store, *sql.DB, string, tenant.Context) {
	t.Helper()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "category_test.db")

	sqlDB, err := db.Open(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	tc := seedTenant(t, sqlDB, prefix)

	store := category.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		nil,
	)
	return store, sqlDB, prefix, tc
}

func seedTenant(t *testing.T, sqlDB *sql.DB, prefix string) tenant.Context {
	t.Helper()
	userID, orgID := uuid.New(), uuid.New()
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
	projID := seedSQLiteProject(t, sqlDB, prefix, userID, orgID, "cash")
	return tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}
}

func seedSQLiteProject(t *testing.T, sqlDB *sql.DB, prefix string, userID, orgID uuid.UUID, slug string) uuid.UUID {
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

// seedSQLiteWallet and seedSQLiteTransaction write raw SQL on purpose: internal/transaction
// is a sibling module, and the only thing these tests need is a referencing row so the
// ON DELETE RESTRICT foreign key fires.
func seedSQLiteWallet(t *testing.T, sqlDB *sql.DB, prefix string, tc tenant.Context) uuid.UUID {
	t.Helper()
	walletID := uuid.New()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"wallets (id, org_id, project_id, name, kind, provider, currency, "+
			"exclude_from_total, archived_at, created_at, updated_at) "+
			"VALUES (?, ?, ?, 'Cash', 'cash', '', 'IDR', 0, NULL, ?, ?)",
		walletID.String(), tc.OrgID.String(), tc.ProjectID.String(), now, now)
	require.NoError(t, err)
	return walletID
}

func seedSQLiteTransaction(t *testing.T, sqlDB *sql.DB, prefix string, tc tenant.Context, walletID, categoryID uuid.UUID) {
	t.Helper()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, "+
			"amount_minor, currency, category_id, period_id, note, note_norm, occurred_at, created_by, "+
			"created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, NULL, 'expense', 1000, 'IDR', ?, NULL, '', '', ?, ?, ?, ?)",
		uuid.NewString(), tc.OrgID.String(), tc.ProjectID.String(), walletID.String(),
		categoryID.String(), now, tc.UserID.String(), now, now)
	require.NoError(t, err)
}

func mustNew(t *testing.T, tc tenant.Context, name string, kind category.Kind) *category.Category {
	t.Helper()
	c, err := category.New(tc.OrgID, tc.ProjectID, name, kind, "", "", 0)
	require.NoError(t, err)
	return c
}

func TestSQLite_SaveAndByID(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	c, err := category.New(tc.OrgID, tc.ProjectID, "Food & Drinks", category.KindExpense, "utensils", "chart-1", 4)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, c))

	got, err := store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, c.ID, got.ID)
	assert.Equal(t, "Food & Drinks", got.Name)
	assert.Equal(t, category.KindExpense, got.Kind)
	assert.Equal(t, "utensils", got.Icon)
	assert.Equal(t, "chart-1", got.Color)
	assert.Equal(t, 4, got.SortOrder)
	assert.Nil(t, got.ArchivedAt)
	assert.Equal(t, tc.OrgID, got.OrgID)
	assert.Equal(t, tc.ProjectID, got.ProjectID)
	assert.WithinDuration(t, c.CreatedAt, got.CreatedAt, time.Millisecond)
}

func TestSQLite_ByID_NotFound(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	_, err := store.ByID(ctx, uuid.New())
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

func TestSQLite_ArchiveRoundTrip(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	c := mustNew(t, tc, "Food", category.KindExpense)
	require.NoError(t, store.Save(ctx, c))

	c.Archive()
	require.NoError(t, store.Save(ctx, c))

	got, err := store.ByID(ctx, c.ID)
	require.NoError(t, err)
	require.True(t, got.IsArchived())
	assert.WithinDuration(t, *c.ArchivedAt, *got.ArchivedAt, time.Millisecond)

	active, err := store.List(ctx, tc.OrgID, tc.ProjectID, category.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, active, "the default list excludes archived rows")

	all, err := store.List(ctx, tc.OrgID, tc.ProjectID, category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Len(t, all, 1)

	c.Unarchive()
	require.NoError(t, store.Save(ctx, c))
	back, err := store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Nil(t, back.ArchivedAt, "archived_at must round-trip back to NULL")
}

func TestSQLite_List_OrderAndFilters(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	seed := []struct {
		name string
		kind category.Kind
		sort int
	}{
		{"Transport", category.KindExpense, 1},
		{"Food", category.KindExpense, 0},
		{"Bravo", category.KindExpense, 2},
		{"Alpha", category.KindExpense, 2},
		{"Salary", category.KindIncome, 0},
	}
	for _, s := range seed {
		c, err := category.New(tc.OrgID, tc.ProjectID, s.name, s.kind, "", "", s.sort)
		require.NoError(t, err)
		require.NoError(t, store.Save(ctx, c))
	}

	all, err := store.List(ctx, tc.OrgID, tc.ProjectID, category.ListOpts{})
	require.NoError(t, err)
	require.Len(t, all, 5)
	assert.Equal(t, []string{"Food", "Salary", "Transport", "Alpha", "Bravo"},
		names(all), "sort_order ASC, then name ASC, then id ASC")

	expense, err := store.List(ctx, tc.OrgID, tc.ProjectID, category.ListOpts{Kind: category.KindExpense})
	require.NoError(t, err)
	assert.Equal(t, []string{"Food", "Transport", "Alpha", "Bravo"}, names(expense))

	income, err := store.List(ctx, tc.OrgID, tc.ProjectID, category.ListOpts{Kind: category.KindIncome})
	require.NoError(t, err)
	assert.Equal(t, []string{"Salary"}, names(income))
}

func names(rows []*category.Category) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func TestSQLite_Save_DuplicateActiveNameInSameKind(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	require.NoError(t, store.Save(ctx, mustNew(t, tc, "Food", category.KindExpense)))

	dup := mustNew(t, tc, "food", category.KindExpense)
	err := store.Save(ctx, dup)
	assert.True(t, category.IsAlreadyExistsError(err), "want AlreadyExistsError, got %T: %v", err, err)
}

func TestSQLite_Save_SameNameUnderTheOtherKind(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	require.NoError(t, store.Save(ctx, mustNew(t, tc, "Other", category.KindExpense)))
	require.NoError(t, store.Save(ctx, mustNew(t, tc, "Other", category.KindIncome)),
		"the unique index is (project_id, kind, lower(name)), not name alone")
}

func TestSQLite_Save_SameNameOnceTheFirstIsArchived(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	first := mustNew(t, tc, "Food", category.KindExpense)
	require.NoError(t, store.Save(ctx, first))
	first.Archive()
	require.NoError(t, store.Save(ctx, first))

	require.NoError(t, store.Save(ctx, mustNew(t, tc, "Food", category.KindExpense)),
		"the unique index is partial: WHERE archived_at IS NULL")
}

func TestSQLite_Save_SameNameInAnotherProject(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	require.NoError(t, store.Save(ctx, mustNew(t, tc, "Food", category.KindExpense)))

	otherProj := seedSQLiteProject(t, sqlDB, prefix, tc.UserID, tc.OrgID, "side")
	sibling, err := category.New(tc.OrgID, otherProj, "Food", category.KindExpense, "", "", 0)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, sibling), "the unique index is scoped to project_id")
}

func TestSQLite_ByIDs(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	a := mustNew(t, tc, "Alpha", category.KindExpense)
	require.NoError(t, store.Save(ctx, a))
	b := mustNew(t, tc, "Beta", category.KindExpense)
	require.NoError(t, store.Save(ctx, b))

	t.Run("unknown ids are simply absent", func(t *testing.T) {
		missing := uuid.New()
		got, err := store.ByIDs(ctx, tc.OrgID, tc.ProjectID, []uuid.UUID{a.ID, missing})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "Alpha", got[a.ID].Name)
		assert.NotContains(t, got, missing)
	})

	t.Run("empty input returns an empty map", func(t *testing.T) {
		got, err := store.ByIDs(ctx, tc.OrgID, tc.ProjectID, nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("another project is out of scope", func(t *testing.T) {
		otherProj := seedSQLiteProject(t, sqlDB, prefix, tc.UserID, tc.OrgID, "side")
		got, err := store.ByIDs(ctx, tc.OrgID, otherProj, []uuid.UUID{a.ID, b.ID})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("archived rows are still resolvable by id", func(t *testing.T) {
		b.Archive()
		require.NoError(t, store.Save(ctx, b))
		got, err := store.ByIDs(ctx, tc.OrgID, tc.ProjectID, []uuid.UUID{b.ID})
		require.NoError(t, err)
		require.Len(t, got, 1, "a transaction still names its archived category")
	})
}

func TestSQLite_Delete(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	c := mustNew(t, tc, "Gone", category.KindExpense)
	require.NoError(t, store.Save(ctx, c))
	require.NoError(t, store.Delete(ctx, c.ID))

	_, err := store.ByID(ctx, c.ID)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError after delete, got %T: %v", err, err)
	assert.True(t, category.IsNotFoundError(store.Delete(ctx, uuid.New())),
		"deleting an unknown id must report NotFoundError")
}

func TestSQLite_Delete_InUse(t *testing.T) {
	store, sqlDB, prefix, tc := newSQLiteFixture(t)
	ctx := tenant.Into(t.Context(), tc)

	c := mustNew(t, tc, "Has Transactions", category.KindExpense)
	require.NoError(t, store.Save(ctx, c))

	walletID := seedSQLiteWallet(t, sqlDB, prefix, tc)
	seedSQLiteTransaction(t, sqlDB, prefix, tc, walletID, c.ID)

	err := store.Delete(ctx, c.ID)
	require.True(t, category.IsInUseError(err), "want InUseError, got %T: %v", err, err)

	got, err := store.ByID(ctx, c.ID)
	require.NoError(t, err, "a refused delete must leave the row in place")
	assert.Equal(t, c.ID, got.ID)
}

func TestSQLite_RequiresTenantScope(t *testing.T) {
	store, _, _, tc := newSQLiteFixture(t)
	c := mustNew(t, tc, "Unscoped", category.KindExpense)

	assert.Error(t, store.Save(context.Background(), c))
	_, err := store.ByID(context.Background(), c.ID)
	assert.Error(t, err)
	_, err = store.ByIDs(context.Background(), tc.OrgID, tc.ProjectID, []uuid.UUID{c.ID})
	assert.Error(t, err)
	_, err = store.List(context.Background(), tc.OrgID, tc.ProjectID, category.ListOpts{})
	assert.Error(t, err)
	assert.Error(t, store.Delete(context.Background(), c.ID))
}

// failAfterStore is a real Store that starts failing writes partway through, so a seed can be
// interrupted mid-flight without stubbing out the database underneath it.
type failAfterStore struct {
	category.Store
	remaining int
	err       error
}

func (f *failAfterStore) Save(ctx context.Context, c *category.Category) error {
	if f.remaining <= 0 {
		return f.err
	}
	f.remaining--
	return f.Store.Save(ctx, c)
}

func countCategories(t *testing.T, sqlDB *sql.DB, prefix string) int {
	t.Helper()
	var n int
	require.NoError(t, sqlDB.QueryRow("SELECT count(*) FROM "+prefix+"categories").Scan(&n))
	return n
}

// TestSQLite_SeedDefaults_EnrollsInTheCallersUnitOfWork is the reason the SQLite store enrolls
// in db.CurrentTx: Phase 2 Task 8 runs SeedDefaults inside the project-creation unit of work, so
// a seed that fails halfway must leave nothing behind. A store that executed against its own
// *sql.DB would write outside the caller's transaction and those rows would survive the rollback.
func TestSQLite_SeedDefaults_EnrollsInTheCallersUnitOfWork(t *testing.T) {
	t.Run("a mid-seed failure rolls the whole seed back", func(t *testing.T) {
		store, sqlDB, prefix, tc := newSQLiteFixture(t)
		boom := errors.New("boom")
		failing := &failAfterStore{Store: store, remaining: 5, err: boom}
		svc, _ := newSvc(t, failing, &stubNamer{})
		ctx := tenant.Into(t.Context(), tc)

		err := db.RunInTx(ctx, db.Pool{W: sqlDB, R: sqlDB}, func(txCtx context.Context) error {
			_, sErr := svc.SeedDefaults(txCtx)
			return sErr
		})
		require.Error(t, err)
		require.ErrorIs(t, err, boom)

		assert.Zero(t, countCategories(t, sqlDB, prefix),
			"the five rows written before the failure must roll back with the caller's unit of work")
	})

	// The positive control: without it the test above would pass against a store that never
	// wrote anything at all.
	t.Run("a successful seed inside a unit of work commits every default", func(t *testing.T) {
		store, sqlDB, prefix, tc := newSQLiteFixture(t)
		svc, _ := newSvc(t, store, &stubNamer{})
		ctx := tenant.Into(t.Context(), tc)

		var seeded int
		err := db.RunInTx(ctx, db.Pool{W: sqlDB, R: sqlDB}, func(txCtx context.Context) error {
			n, sErr := svc.SeedDefaults(txCtx)
			seeded = n
			return sErr
		})
		require.NoError(t, err)
		assert.Equal(t, len(category.Defaults), seeded)
		assert.Equal(t, len(category.Defaults), countCategories(t, sqlDB, prefix))
	})

	t.Run("reads inside the unit of work see its uncommitted writes", func(t *testing.T) {
		store, sqlDB, _, tc := newSQLiteFixture(t)
		ctx := tenant.Into(t.Context(), tc)

		err := db.RunInTx(ctx, db.Pool{W: sqlDB, R: sqlDB}, func(txCtx context.Context) error {
			c := mustNew(t, tc, "Food", category.KindExpense)
			if sErr := store.Save(txCtx, c); sErr != nil {
				return sErr
			}
			got, bErr := store.ByID(txCtx, c.ID)
			if bErr != nil {
				return bErr
			}
			require.Equal(t, "Food", got.Name, "a read must enroll too, or it cannot see the write")

			rows, lErr := store.List(txCtx, tc.OrgID, tc.ProjectID, category.ListOpts{})
			if lErr != nil {
				return lErr
			}
			require.Len(t, rows, 1)

			byIDs, iErr := store.ByIDs(txCtx, tc.OrgID, tc.ProjectID, []uuid.UUID{c.ID})
			if iErr != nil {
				return iErr
			}
			require.Len(t, byIDs, 1)
			return nil
		})
		require.NoError(t, err)
	})
}
