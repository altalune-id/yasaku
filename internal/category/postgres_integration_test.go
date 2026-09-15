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

	"altalune.id/yasaku/internal/category"
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
	projID := seedPgProject(t, sqlDB, prefix, userID, orgID, "cash")

	store := category.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		tenant.NewPgConn(sqlDB),
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

// seedPgWallet and seedPgTransaction write raw SQL on purpose: internal/transaction is a
// sibling module, and all these tests need is a referencing row so ON DELETE RESTRICT fires.
func seedPgWallet(t *testing.T, sqlDB *sql.DB, prefix string, tc tenant.Context) uuid.UUID {
	t.Helper()
	walletID := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"wallets (id, org_id, project_id, name, kind, provider, currency, "+
			"exclude_from_total, archived_at, created_at, updated_at) "+
			"VALUES ($1, $2, $3, 'Cash', 'cash', '', 'IDR', false, NULL, $4, $4)",
		walletID, tc.OrgID, tc.ProjectID, now)
	require.NoError(t, err)
	return walletID
}

func seedPgTransaction(t *testing.T, sqlDB *sql.DB, prefix string, tc tenant.Context, walletID, categoryID uuid.UUID) {
	t.Helper()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"transactions (id, org_id, project_id, wallet_id, to_wallet_id, kind, "+
			"amount_minor, currency, category_id, period_id, note, note_norm, occurred_at, created_by, "+
			"created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, NULL, 'expense', 1000, 'IDR', $5, NULL, '', '', $6, $7, $6, $6)",
		uuid.New(), tc.OrgID, tc.ProjectID, walletID, categoryID, now, tc.UserID)
	require.NoError(t, err)
}

func pgNew(t *testing.T, tc tenant.Context, name string, kind category.Kind) *category.Category {
	t.Helper()
	c, err := category.New(tc.OrgID, tc.ProjectID, name, kind, "", "", 0)
	require.NoError(t, err)
	return c
}

func TestPostgres_Category_SaveAndByID(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	c, err := category.New(f.tc.OrgID, f.tc.ProjectID, "Food & Drinks", category.KindExpense, "utensils", "chart-1", 4)
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, c))

	got, err := f.store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, c.ID, got.ID)
	assert.Equal(t, "Food & Drinks", got.Name)
	assert.Equal(t, category.KindExpense, got.Kind)
	assert.Equal(t, "utensils", got.Icon)
	assert.Equal(t, "chart-1", got.Color)
	assert.Equal(t, 4, got.SortOrder)
	assert.Nil(t, got.ArchivedAt)
	assert.Equal(t, f.tc.ProjectID, got.ProjectID)
}

func TestPostgres_Category_ByID_NotFound(t *testing.T) {
	f := newPgFixture(t)
	_, err := f.store.ByID(tenant.Into(t.Context(), f.tc), uuid.New())
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

func TestPostgres_Category_ArchiveRoundTrip(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	c := pgNew(t, f.tc, "Food", category.KindExpense)
	require.NoError(t, f.store.Save(ctx, c))

	c.Archive()
	require.NoError(t, f.store.Save(ctx, c))
	got, err := f.store.ByID(ctx, c.ID)
	require.NoError(t, err)
	require.True(t, got.IsArchived())
	assert.WithinDuration(t, *c.ArchivedAt, *got.ArchivedAt, time.Millisecond)

	active, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, category.ListOpts{})
	require.NoError(t, err)
	assert.Empty(t, active)

	all, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Len(t, all, 1)

	c.Unarchive()
	require.NoError(t, f.store.Save(ctx, c))
	back, err := f.store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Nil(t, back.ArchivedAt, "archived_at must round-trip back to NULL")
}

func TestPostgres_Category_List_OrderAndFilters(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

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
		c, err := category.New(f.tc.OrgID, f.tc.ProjectID, s.name, s.kind, "", "", s.sort)
		require.NoError(t, err)
		require.NoError(t, f.store.Save(ctx, c))
	}

	all, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, category.ListOpts{})
	require.NoError(t, err)
	require.Len(t, all, 5)
	assert.Equal(t, []string{"Food", "Salary", "Transport", "Alpha", "Bravo"}, names(all),
		"sort_order ASC, then name ASC, then id ASC")

	expense, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, category.ListOpts{Kind: category.KindExpense})
	require.NoError(t, err)
	assert.Equal(t, []string{"Food", "Transport", "Alpha", "Bravo"}, names(expense))
}

func TestPostgres_Category_UniqueIndexIsPartialAndScoped(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	first := pgNew(t, f.tc, "Food", category.KindExpense)
	require.NoError(t, f.store.Save(ctx, first))

	dup := pgNew(t, f.tc, "food", category.KindExpense)
	assert.True(t, category.IsAlreadyExistsError(f.store.Save(ctx, dup)),
		"a repeated lower(name) in one kind must be AlreadyExistsError")

	require.NoError(t, f.store.Save(ctx, pgNew(t, f.tc, "Food", category.KindIncome)),
		"the unique index is (project_id, kind, lower(name))")

	otherProj := seedPgProject(t, f.sqlDB, f.prefix, f.tc.UserID, f.tc.OrgID, "side")
	sibling, err := category.New(f.tc.OrgID, otherProj, "Food", category.KindExpense, "", "", 0)
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, sibling), "the unique index is scoped to project_id")

	first.Archive()
	require.NoError(t, f.store.Save(ctx, first))
	require.NoError(t, f.store.Save(ctx, pgNew(t, f.tc, "Food", category.KindExpense)),
		"the unique index is partial: WHERE archived_at IS NULL")
}

func TestPostgres_Category_ByIDs(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	a := pgNew(t, f.tc, "Alpha", category.KindExpense)
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

	otherProj := seedPgProject(t, f.sqlDB, f.prefix, f.tc.UserID, f.tc.OrgID, "side")
	outOfScope, err := f.store.ByIDs(ctx, f.tc.OrgID, otherProj, []uuid.UUID{a.ID})
	require.NoError(t, err)
	assert.Empty(t, outOfScope, "another project is out of scope")
}

func TestPostgres_Category_Delete(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	c := pgNew(t, f.tc, "Gone", category.KindExpense)
	require.NoError(t, f.store.Save(ctx, c))
	require.NoError(t, f.store.Delete(ctx, c.ID))

	_, err := f.store.ByID(ctx, c.ID)
	assert.True(t, category.IsNotFoundError(err), "want NotFoundError after delete, got %T: %v", err, err)
	assert.True(t, category.IsNotFoundError(f.store.Delete(ctx, uuid.New())))
}

func TestPostgres_Category_Delete_InUse(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	c := pgNew(t, f.tc, "Has Transactions", category.KindExpense)
	require.NoError(t, f.store.Save(ctx, c))

	walletID := seedPgWallet(t, f.sqlDB, f.prefix, f.tc)
	seedPgTransaction(t, f.sqlDB, f.prefix, f.tc, walletID, c.ID)

	err := f.store.Delete(ctx, c.ID)
	require.True(t, category.IsInUseError(err), "want InUseError, got %T: %v", err, err)

	got, err := f.store.ByID(ctx, c.ID)
	require.NoError(t, err, "a refused delete must leave the row in place")
	assert.Equal(t, c.ID, got.ID)
}

// TestPostgres_Category_Save_CannotUpsertOntoAnotherOrgsRow runs on the plain superuser
// fixture ON PURPOSE. RLS is bypassed there, so Save's conflict-clause org guard is the only
// thing preventing the hijack — which is what makes this test able to detect its removal.
func TestPostgres_Category_Save_CannotUpsertOntoAnotherOrgsRow(t *testing.T) {
	f := newPgFixture(t)
	ownerCtx := tenant.Into(t.Context(), f.tc)

	c := pgNew(t, f.tc, "Org A Only", category.KindExpense)
	require.NoError(t, f.store.Save(ownerCtx, c))

	otherUser, otherOrg := seedPgUserAndOrg(t, f.sqlDB, f.prefix)
	otherProj := seedPgProject(t, f.sqlDB, f.prefix, otherUser, otherOrg, "cash")
	otherCtx := tenant.Into(t.Context(), tenant.Context{OrgID: otherOrg, ProjectID: otherProj, UserID: otherUser})

	verbatim := *c
	verbatim.Name = "Hijacked"
	assert.True(t, category.IsNotFoundError(f.store.Save(otherCtx, &verbatim)),
		"org B must not be able to upsert onto org A's row")

	reskinned := *c
	reskinned.OrgID, reskinned.ProjectID = otherOrg, otherProj
	reskinned.Name = "Hijacked"
	assert.True(t, category.IsNotFoundError(f.store.Save(otherCtx, &reskinned)),
		"org B must not be able to upsert onto org A's row id under its own org")

	assert.True(t, category.IsNotFoundError(f.store.Delete(otherCtx, c.ID)),
		"org B must not be able to delete org A's row")

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

	c := pgNew(t, f.tc, "Food", category.KindExpense)
	require.NoError(t, f.store.Save(ctx, c))

	require.NoError(t, c.Rename("Groceries"))
	require.NoError(t, f.store.Save(ctx, c), "the owning tenant must still be able to update its own row")

	got, err := f.store.ByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "Groceries", got.Name)
}

// TestPostgres_Category_OtherOrgIsInvisible runs on the RLS fixture, not the superuser one,
// so it proves tenant isolation rather than only the store's own org_id predicate.
func TestPostgres_Category_OtherOrgIsInvisible(t *testing.T) {
	store, migDB, prefix := newPgRLSFixture(t)
	a := seedRLSTenant(t, migDB, prefix)
	b := seedRLSTenant(t, migDB, prefix)

	ownerCtx := tenant.Into(t.Context(), a)
	otherCtx := tenant.Into(t.Context(), b)

	c, err := category.New(a.OrgID, a.ProjectID, "Org A Only", category.KindExpense, "", "", 0)
	require.NoError(t, err)
	require.NoError(t, store.Save(ownerCtx, c))

	_, err = store.ByID(otherCtx, c.ID)
	assert.True(t, category.IsNotFoundError(err), "org B must not see org A's category, got %T: %v", err, err)

	listed, err := store.List(otherCtx, b.OrgID, b.ProjectID, category.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Empty(t, listed)

	byIDs, err := store.ByIDs(otherCtx, a.OrgID, a.ProjectID, []uuid.UUID{c.ID})
	require.NoError(t, err)
	assert.Empty(t, byIDs, "org B must not read org A's rows even when it names org A's scope")

	assert.True(t, category.IsNotFoundError(store.Delete(otherCtx, c.ID)),
		"org B must not be able to delete org A's category")

	// Two hijack shapes refused by two different layers: org A's org_id trips the policy's
	// WITH CHECK (SQLSTATE 42501); org B's own org_id passes WITH CHECK and is stopped by
	// Save's conflict-clause org guard instead.
	verbatim := *c
	verbatim.Name = "Hijacked"
	assert.Error(t, store.Save(otherCtx, &verbatim))

	reskinned := *c
	reskinned.OrgID, reskinned.ProjectID = b.OrgID, b.ProjectID
	reskinned.Name = "Hijacked"
	assert.Error(t, store.Save(otherCtx, &reskinned))

	stillThere, err := store.ByID(ownerCtx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "Org A Only", stillThere.Name, "org A's row must be untouched")
	assert.Equal(t, a.OrgID, stillThere.OrgID)
}

// newPgRLSFixture migrates under a BYPASSRLS owner with Tenant.RLSEnforce on, then binds the
// store to a NOBYPASSRLS LOGIN role so the tenant policies actually apply. newPgFixture above
// connects as the container superuser, which bypasses row level security outright.
func newPgRLSFixture(t *testing.T) (category.Store, *sql.DB, string) {
	t.Helper()
	h := pgtest.New(t)
	suffix := uniqueSuffix(t)
	ownerRole := "yasaku_txcatowner_" + suffix
	appRole := "yasaku_txcatapp_" + suffix
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
	requirePoliciesExist(t, migDB, prefix+"categories")

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

// requirePoliciesExist fails loudly when RLSEnforce did not actually render, so a cross-org
// test can never pass merely because no policy was created.
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
			"VALUES ($1, $2, 'cash', 'Cash', $3, $4, $4)",
		projID, orgID, userID, now)
	require.NoError(t, err)
	return tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID}
}
