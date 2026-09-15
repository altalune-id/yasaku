//go:build integration

package wallet_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

type pgFixture struct {
	store  wallet.Store
	db     *sql.DB
	prefix string
	tc     tenant.Context
}

func newPgFixture(t *testing.T) pgFixture {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	prefix := cfg.DB.TablePrefix
	userID, orgID, projID := seedPgProjectTree(t, sqlDB, prefix)
	store := wallet.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: prefix},
		db.Pool{W: sqlDB, R: sqlDB},
		tenant.NewPgConn(sqlDB),
	)
	return pgFixture{
		store:  store,
		db:     sqlDB,
		prefix: prefix,
		tc:     tenant.Context{OrgID: orgID, ProjectID: projID, UserID: userID},
	}
}

func seedPgProjectTree(t *testing.T, sqlDB *sql.DB, prefix string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	userID, orgID, projID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
		userID, userID.String()+"@x.co", now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES ($1, 'acme', 'Acme', $2, $3, $3)",
		orgID, userID, now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'web', 'Web', $3, $4, $4)",
		projID, orgID, userID, now)
	require.NoError(t, err)
	return userID, orgID, projID
}

// seedPgTransaction inserts a raw transactions row referencing walletID. NOTE: raw SQL because
// internal/transaction is a sibling module this package must not import.
func seedPgTransaction(t *testing.T, f pgFixture, walletID uuid.UUID) {
	t.Helper()
	now := time.Now().UTC()
	_, err := f.db.ExecContext(t.Context(),
		"INSERT INTO "+f.prefix+"transactions "+
			"(id, org_id, project_id, wallet_id, kind, amount_minor, currency, note, note_norm, occurred_at, created_by, created_at, updated_at) "+
			"VALUES ($1, $2, $3, $4, 'opening', 100000, 'IDR', '', '', $5, $6, $5, $5)",
		uuid.New(), f.tc.OrgID, f.tc.ProjectID, walletID, now, f.tc.UserID)
	require.NoError(t, err)
}

func newPgWallet(t *testing.T, f pgFixture, name string) *wallet.Wallet {
	t.Helper()
	w, err := wallet.New(f.tc.OrgID, f.tc.ProjectID, wallet.Params{
		Name: name, Kind: wallet.KindBank, Provider: "Bank Central Asia", Currency: money.IDR,
	})
	require.NoError(t, err)
	require.NoError(t, f.store.Save(tenant.Into(t.Context(), f.tc), w))
	return w
}

func TestPostgres_Wallet_RoundTrip(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	w, err := wallet.New(f.tc.OrgID, f.tc.ProjectID, wallet.Params{
		Name: "BCA", Kind: wallet.KindSavings, Provider: "Bank Central Asia",
		Currency: money.IDR, ExcludeFromTotal: true,
	})
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, w))

	got, err := f.store.ByID(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, "BCA", got.Name)
	assert.Equal(t, wallet.KindSavings, got.Kind)
	assert.Equal(t, "Bank Central Asia", got.Provider)
	assert.Equal(t, money.IDR, got.Currency)
	assert.True(t, got.ExcludeFromTotal)
	assert.Nil(t, got.ArchivedAt)

	w.Archive()
	require.NoError(t, f.store.Save(ctx, w))
	archived, err := f.store.ByID(ctx, w.ID)
	require.NoError(t, err)
	require.NotNil(t, archived.ArchivedAt)
	assert.True(t, archived.ArchivedAt.Equal(*w.ArchivedAt))

	archived.Unarchive()
	require.NoError(t, f.store.Save(ctx, archived))
	back, err := f.store.ByID(ctx, w.ID)
	require.NoError(t, err)
	assert.Nil(t, back.ArchivedAt, "unarchive must write NULL")
}

func TestPostgres_Wallet_NotFound(t *testing.T) {
	f := newPgFixture(t)
	_, err := f.store.ByID(tenant.Into(t.Context(), f.tc), uuid.New())
	assert.True(t, wallet.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestPostgres_Wallet_UniqueNamePerProjectIgnoringCase(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	newPgWallet(t, f, "BCA")

	dup, err := wallet.New(f.tc.OrgID, f.tc.ProjectID, wallet.Params{
		Name: "bca", Kind: wallet.KindBank, Currency: money.IDR,
	})
	require.NoError(t, err)
	err = f.store.Save(ctx, dup)
	require.True(t, wallet.IsAlreadyExistsError(err), "got %T: %v", err, err)
	var exists *wallet.AlreadyExistsError
	require.ErrorAs(t, err, &exists)
	assert.Equal(t, "bca", exists.Name)
}

func TestPostgres_Wallet_ArchivingFreesTheName(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	first := newPgWallet(t, f, "BCA")
	first.Archive()
	require.NoError(t, f.store.Save(ctx, first))

	second := newPgWallet(t, f, "BCA")
	require.NotEqual(t, first.ID, second.ID)

	first.Unarchive()
	err := f.store.Save(ctx, first)
	assert.True(t, wallet.IsAlreadyExistsError(err), "got %T: %v", err, err)
}

func TestPostgres_Wallet_ListOrderAndArchivedFilter(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)

	newPgWallet(t, f, "Mandiri")
	newPgWallet(t, f, "BCA")
	gone := newPgWallet(t, f, "Jenius")
	gone.Archive()
	require.NoError(t, f.store.Save(ctx, gone))

	active, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, wallet.ListOpts{})
	require.NoError(t, err)
	names := make([]string, 0, len(active))
	for _, w := range active {
		names = append(names, w.Name)
	}
	assert.Equal(t, []string{"BCA", "Mandiri"}, names)

	all, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, wallet.ListOpts{IncludeArchived: true})
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestPostgres_Wallet_DeleteRestrictBecomesInUse(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	w := newPgWallet(t, f, "BCA")
	seedPgTransaction(t, f, w.ID)

	err := f.store.Delete(ctx, w.ID)
	require.True(t, wallet.IsInUseError(err), "got %T: %v", err, err)
	var inUse *wallet.InUseError
	require.ErrorAs(t, err, &inUse)
	assert.Equal(t, w.ID.String(), inUse.ID)

	_, err = f.store.ByID(ctx, w.ID)
	require.NoError(t, err, "a refused delete must leave the row in place")
}

func TestPostgres_Wallet_DeleteUnreferenced(t *testing.T) {
	f := newPgFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	w := newPgWallet(t, f, "BCA")

	require.NoError(t, f.store.Delete(ctx, w.ID))
	_, err := f.store.ByID(ctx, w.ID)
	assert.True(t, wallet.IsNotFoundError(err))
	assert.True(t, wallet.IsNotFoundError(f.store.Delete(ctx, w.ID)))
}
