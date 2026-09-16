//go:build integration

package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

type pgFixture struct {
	store  ledger.Store
	sqlDB  *sql.DB
	prefix string
	tc     tenant.Context
}

// newPgFixture builds the plain superuser fixture. The migration does create RLS policies, but
// this connection bypasses them, so the store's own org predicates are the only tenant
// protection — which is what lets the hijack test detect their removal.
func newPgFixture(t *testing.T) pgFixture {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	requireBypassesRLS(t, sqlDB)

	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverPostgres
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	userID, orgID, projID := seedProjectTree(t, sqlDB, prefix)

	store := ledger.NewStore(
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

// requireBypassesRLS fails the fixture unless this connection really is exempt from row level
// security. Without it the guard tests below would pass whether or not the guard exists, because
// RLS would refuse the cross-tenant write either way.
func requireBypassesRLS(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	var bypasses bool
	require.NoError(t, sqlDB.QueryRowContext(t.Context(),
		"SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user").Scan(&bypasses))
	require.True(t, bypasses,
		"this fixture's role must bypass RLS, or the upsert guard tests below cannot detect the guard's removal")
}

func seedProjectTree(t *testing.T, sqlDB *sql.DB, prefix string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	userID := uuid.New()
	orgID := uuid.New()
	projID := uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
		userID, userID.String()+"@x.co", now,
	)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'Acme', $3, $4, $4)",
		orgID, orgID.String()[:8], userID, now,
	)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Web', $4, $5, $5)",
		projID, orgID, projID.String()[:8], userID, now,
	)
	require.NoError(t, err)
	return userID, orgID, projID
}

func pgConfigured(t *testing.T, tc tenant.Context, tz string, day int) *ledger.Settings {
	t.Helper()
	st := ledger.Defaults(tc.OrgID, tc.ProjectID)
	require.NoError(t, st.Apply(ledger.Patch{Timezone: &tz, PeriodStartDay: &day}))
	return st
}

func TestPostgres_Ledger_SaveAndByProject(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	want := ledger.Defaults(f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, want.Apply(ledger.Patch{
		Timezone:       ptr("Asia/Tokyo"),
		Currency:       ptr(money.Currency("USD")),
		PeriodStartDay: ptr(25),
	}))
	require.NoError(t, f.store.Save(ctx, want))

	got, err := f.store.ByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, f.tc.OrgID, got.OrgID)
	assert.Equal(t, f.tc.ProjectID, got.ProjectID)
	assert.Equal(t, "Asia/Tokyo", got.Timezone)
	assert.Equal(t, money.Currency("USD"), got.Currency)
	assert.Equal(t, 25, got.PeriodStartDay)
	// NOTE: timestamptz is microsecond precision, and time.Now() carries nanoseconds on Linux.
	assert.True(t, got.UpdatedAt.Equal(want.UpdatedAt.Truncate(time.Microsecond)),
		"UpdatedAt got=%v want=%v", got.UpdatedAt, want.UpdatedAt.Truncate(time.Microsecond))
}

func TestPostgres_Ledger_SaveIsUpsert(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	st := pgConfigured(t, f.tc, "Asia/Jakarta", 1)
	require.NoError(t, f.store.Save(ctx, st))
	require.NoError(t, st.Apply(ledger.Patch{Timezone: ptr("Europe/Berlin"), PeriodStartDay: ptr(10)}))
	require.NoError(t, f.store.Save(ctx, st))

	got, err := f.store.ByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, "Europe/Berlin", got.Timezone)
	assert.Equal(t, 10, got.PeriodStartDay)
}

func TestPostgres_Ledger_NotFound(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	_, err := f.store.ByProject(ctx, f.tc.OrgID, uuid.New())
	assert.True(t, ledger.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

func TestPostgres_Ledger_ForeignOrgIsInvisible(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	require.NoError(t, f.store.Save(ctx, pgConfigured(t, f.tc, "Asia/Jakarta", 1)))

	_, err := f.store.ByProject(ctx, uuid.New(), f.tc.ProjectID)
	assert.True(t, ledger.IsNotFoundError(err), "want NotFoundError, got %T: %v", err, err)
}

// TestPostgres_Ledger_Save_CannotUpsertOntoAnotherOrgsRow runs on the plain superuser fixture ON
// PURPOSE. RLS is bypassed there, so Save's conflict-clause org guard and its RowsAffected()==0
// branch are the only things preventing the hijack — which is what makes this test able to
// detect their removal.
func TestPostgres_Ledger_Save_CannotUpsertOntoAnotherOrgsRow(t *testing.T) {
	f := newPgFixture(t)
	ownerCtx := tenant.Into(t.Context(), f.tc)

	victim := pgConfigured(t, f.tc, "Asia/Jakarta", 3)
	require.NoError(t, f.store.Save(ownerCtx, victim))

	otherUser, otherOrg, otherProj := seedProjectTree(t, f.sqlDB, f.prefix)
	otherTC := tenant.Context{OrgID: otherOrg, ProjectID: otherProj, UserID: otherUser}
	otherCtx := tenant.Into(t.Context(), otherTC)

	verbatim := *victim
	verbatim.Timezone = "Europe/Berlin"
	verbatim.PeriodStartDay = 28
	verbatim.UpdatedAt = time.Now().UTC()
	assert.True(t, ledger.IsNotFoundError(f.store.Save(otherCtx, &verbatim)),
		"org B replaying org A's row verbatim must be refused")

	reskinned := *victim
	reskinned.OrgID = otherOrg
	reskinned.Timezone = "Europe/Berlin"
	reskinned.PeriodStartDay = 28
	reskinned.UpdatedAt = time.Now().UTC()
	assert.True(t, ledger.IsNotFoundError(f.store.Save(otherCtx, &reskinned)),
		"org B naming org A's project id under its own org must be refused")

	got, err := f.store.ByProject(ownerCtx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err, "a refused upsert must leave org A's row in place")
	assert.Equal(t, victim.OrgID, got.OrgID)
	assert.Equal(t, victim.ProjectID, got.ProjectID)
	assert.Equal(t, victim.Timezone, got.Timezone, "org A's row must be untouched")
	assert.Equal(t, victim.Currency, got.Currency, "org A's row must be untouched")
	assert.Equal(t, victim.PeriodStartDay, got.PeriodStartDay, "org A's row must be untouched")
	assert.True(t, got.UpdatedAt.Equal(victim.UpdatedAt.Truncate(time.Microsecond)),
		"org A's UpdatedAt must be untouched: got=%v want=%v", got.UpdatedAt, victim.UpdatedAt.Truncate(time.Microsecond))

	assert.Zero(t, pgRowCount(t, f, otherProj), "the refused upsert must not have inserted a row for org B either")
}

// TestPostgres_Ledger_Save_UpdatesOwnRow is the positive control: a WHERE(false) conflict guard
// would pass every hijack assertion above while breaking this one.
func TestPostgres_Ledger_Save_UpdatesOwnRow(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	st := pgConfigured(t, f.tc, "Asia/Jakarta", 3)
	require.NoError(t, f.store.Save(ctx, st))

	require.NoError(t, st.Apply(ledger.Patch{Timezone: ptr("Europe/Berlin"), PeriodStartDay: ptr(28)}))
	require.NoError(t, f.store.Save(ctx, st), "the owning tenant must still be able to update its own row")

	got, err := f.store.ByProject(ctx, f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, "Europe/Berlin", got.Timezone)
	assert.Equal(t, 28, got.PeriodStartDay)
}

func pgRowCount(t *testing.T, f pgFixture, projectID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, f.sqlDB.QueryRowContext(t.Context(),
		"SELECT count(*) FROM "+f.prefix+"ledger_settings WHERE project_id = $1", projectID).Scan(&n))
	return n
}

// TestPostgres_Ledger_Save_EnrollsInTheCallersUnitOfWork pins that the adapter joins an outer
// unit of work rather than opening its own transaction, which is what Task 8's real UnitOfWork
// depends on: a later failure in the same unit must roll the settings row back with it.
func TestPostgres_Ledger_Save_EnrollsInTheCallersUnitOfWork(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	pool := db.Pool{W: f.sqlDB, R: f.sqlDB}

	st := pgConfigured(t, f.tc, "Asia/Tokyo", 25)
	laterStepFailed := errors.New("a later step in the unit of work failed")

	err := db.RunInTx(ctx, pool, func(ctx context.Context) error {
		require.NoError(t, f.store.Save(ctx, st))
		return laterStepFailed
	})
	require.ErrorIs(t, err, laterStepFailed)

	assert.Zero(t, pgRowCount(t, f, f.tc.ProjectID),
		"the settings row survived the unit of work's rollback, so Save did not enroll in it")

	require.NoError(t, db.RunInTx(ctx, pool, func(ctx context.Context) error {
		return f.store.Save(ctx, st)
	}), "a committing unit of work must still persist the row")
	assert.Equal(t, 1, pgRowCount(t, f, f.tc.ProjectID))
}
