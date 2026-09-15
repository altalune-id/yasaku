package wallet_test

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
	_ "modernc.org/sqlite"

	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

type sqliteFixture struct {
	store  wallet.Store
	db     *sql.DB
	pool   db.Pool
	prefix string
	tc     tenant.Context
}

func (f sqliteFixture) ctx(t *testing.T) context.Context {
	t.Helper()
	return tenant.Into(t.Context(), f.tc)
}

// newSQLiteFixture opens a file-backed database through db.Open so every connection carries
// foreign_keys(1) and a caller-held transaction is visible to a second statement.
func newSQLiteFixture(t *testing.T) sqliteFixture {
	t.Helper()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "wallet.db")

	sqlDB, err := db.Open(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	uid, oid, pid := seedSQLiteTenant(t, sqlDB, cfg.DB.TablePrefix)
	pool := db.Pool{W: sqlDB, R: sqlDB}
	store := wallet.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix},
		pool,
		nil,
	)
	return sqliteFixture{
		store:  store,
		db:     sqlDB,
		pool:   pool,
		prefix: cfg.DB.TablePrefix,
		tc:     tenant.Context{OrgID: oid, ProjectID: pid, UserID: uid},
	}
}

func seedSQLiteTenant(t *testing.T, sqlDB *sql.DB, prefix string) (userID, orgID, projID uuid.UUID) { //nolint:nonamedreturns // triple
	t.Helper()
	userID, orgID, projID = uuid.New(), uuid.New(), uuid.New()
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
	return
}

// seedSQLiteTransaction inserts a raw transactions row referencing walletID. NOTE: raw SQL because
// internal/transaction is a sibling module this package must not import.
func seedSQLiteTransaction(t *testing.T, f sqliteFixture, walletID uuid.UUID) {
	t.Helper()
	now := sqliteent.SQLiteTime(time.Now())
	_, err := f.db.Exec(
		"INSERT INTO "+f.prefix+"transactions "+
			"(id, org_id, project_id, wallet_id, kind, amount_minor, currency, note, note_norm, occurred_at, created_by, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, 'opening', 100000, 'IDR', '', '', ?, ?, ?, ?)",
		uuid.New().String(), f.tc.OrgID.String(), f.tc.ProjectID.String(), walletID.String(),
		now, f.tc.UserID.String(), now, now)
	require.NoError(t, err)
}

func newSQLiteWallet(t *testing.T, f sqliteFixture, name string) *wallet.Wallet {
	t.Helper()
	w, err := wallet.New(f.tc.OrgID, f.tc.ProjectID, wallet.Params{
		Name: name, Kind: wallet.KindBank, Provider: "Bank Central Asia", Currency: money.IDR,
	})
	require.NoError(t, err)
	require.NoError(t, f.store.Save(f.ctx(t), w))
	return w
}

func TestSQLiteStore_RoundTrip(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)

	w, err := wallet.New(f.tc.OrgID, f.tc.ProjectID, wallet.Params{
		Name: "BCA", Kind: wallet.KindSavings, Provider: "Bank Central Asia",
		Currency: money.IDR, ExcludeFromTotal: true,
	})
	require.NoError(t, err)
	require.NoError(t, f.store.Save(ctx, w))

	got, err := f.store.ByID(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, w.ID, got.ID)
	assert.Equal(t, f.tc.OrgID, got.OrgID)
	assert.Equal(t, f.tc.ProjectID, got.ProjectID)
	assert.Equal(t, "BCA", got.Name)
	assert.Equal(t, wallet.KindSavings, got.Kind)
	assert.Equal(t, "Bank Central Asia", got.Provider)
	assert.Equal(t, money.IDR, got.Currency)
	assert.True(t, got.ExcludeFromTotal)
	assert.Nil(t, got.ArchivedAt)
	assert.True(t, got.CreatedAt.Equal(w.CreatedAt), "created_at round trip")
	assert.True(t, got.UpdatedAt.Equal(w.UpdatedAt), "updated_at round trip")
}

func TestSQLiteStore_ArchivedAtRoundTrip(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)
	w := newSQLiteWallet(t, f, "Jenius")

	w.Archive()
	require.NoError(t, f.store.Save(ctx, w))

	got, err := f.store.ByID(ctx, w.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ArchivedAt)
	assert.True(t, got.ArchivedAt.Equal(*w.ArchivedAt))

	got.Unarchive()
	require.NoError(t, f.store.Save(ctx, got))
	back, err := f.store.ByID(ctx, w.ID)
	require.NoError(t, err)
	assert.Nil(t, back.ArchivedAt, "unarchive must write NULL, not the old timestamp")
}

func TestSQLiteStore_ByID_NotFound(t *testing.T) {
	f := newSQLiteFixture(t)
	_, err := f.store.ByID(f.ctx(t), uuid.New())
	assert.True(t, wallet.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestSQLiteStore_UniqueNamePerProjectIgnoringCase(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)
	newSQLiteWallet(t, f, "BCA")

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

func TestSQLiteStore_ArchivingFreesTheName(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)

	first := newSQLiteWallet(t, f, "BCA")
	first.Archive()
	require.NoError(t, f.store.Save(ctx, first))

	second := newSQLiteWallet(t, f, "BCA")
	require.NotEqual(t, first.ID, second.ID)

	first.Unarchive()
	err := f.store.Save(ctx, first)
	assert.True(t, wallet.IsAlreadyExistsError(err), "unarchiving onto a taken name must collide, got %T: %v", err, err)
}

// TestSQLiteStore_RenamingArchivedEscapesCollision proves the escape against the real partial
// unique index, not just the aggregate: renaming the archived row frees the unarchive.
func TestSQLiteStore_RenamingArchivedEscapesCollision(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)

	first := newSQLiteWallet(t, f, "BCA")
	first.Archive()
	require.NoError(t, f.store.Save(ctx, first))
	newSQLiteWallet(t, f, "BCA")

	first.Unarchive()
	require.True(t, wallet.IsAlreadyExistsError(f.store.Save(ctx, first)),
		"fixture: the collision must exist first")

	first.Archive()
	require.NoError(t, first.Rename("BCA (closed)"))
	require.NoError(t, f.store.Save(ctx, first))

	first.Unarchive()
	require.NoError(t, f.store.Save(ctx, first))

	got, err := f.store.ByID(ctx, first.ID)
	require.NoError(t, err)
	assert.Nil(t, got.ArchivedAt)
	assert.Equal(t, "BCA (closed)", got.Name)
}

func TestSQLiteStore_ListOrderAndArchivedFilter(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)

	newSQLiteWallet(t, f, "Mandiri")
	newSQLiteWallet(t, f, "BCA")
	gone := newSQLiteWallet(t, f, "Jenius")
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

func TestSQLiteStore_Delete(t *testing.T) {
	t.Run("removes an unreferenced wallet", func(t *testing.T) {
		f := newSQLiteFixture(t)
		ctx := f.ctx(t)
		w := newSQLiteWallet(t, f, "BCA")

		require.NoError(t, f.store.Delete(ctx, w.ID))
		_, err := f.store.ByID(ctx, w.ID)
		assert.True(t, wallet.IsNotFoundError(err))
	})

	t.Run("a wallet with transactions is InUseError", func(t *testing.T) {
		f := newSQLiteFixture(t)
		ctx := f.ctx(t)
		w := newSQLiteWallet(t, f, "BCA")
		seedSQLiteTransaction(t, f, w.ID)

		err := f.store.Delete(ctx, w.ID)
		require.True(t, wallet.IsInUseError(err), "got %T: %v", err, err)
		var inUse *wallet.InUseError
		require.ErrorAs(t, err, &inUse)
		assert.Equal(t, w.ID.String(), inUse.ID)

		_, err = f.store.ByID(ctx, w.ID)
		require.NoError(t, err, "a refused delete must leave the row in place")
	})

	t.Run("an unknown id is NotFoundError", func(t *testing.T) {
		f := newSQLiteFixture(t)
		assert.True(t, wallet.IsNotFoundError(f.store.Delete(f.ctx(t), uuid.New())))
	})
}

// TestSQLiteStore_RollsBackWithCallerTransaction is the atomicity OpenWorkflow.Run depends on:
// a wallet written inside a real db.RunInTx must vanish when a later write in that unit of work
// fails. A store that ignores db.CurrentTx commits the insert on its own connection and passes
// every pass-through-fake test while failing this one.
func TestSQLiteStore_RollsBackWithCallerTransaction(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)

	w, err := wallet.New(f.tc.OrgID, f.tc.ProjectID, wallet.Params{
		Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR,
	})
	require.NoError(t, err)

	boom := errors.New("opening balance failed")
	err = db.RunInTx(ctx, f.pool, func(ctx context.Context) error {
		if sErr := f.store.Save(ctx, w); sErr != nil {
			return sErr
		}
		return boom
	})
	require.ErrorIs(t, err, boom)

	_, err = f.store.ByID(f.ctx(t), w.ID)
	assert.True(t, wallet.IsNotFoundError(err),
		"the wallet must roll back with its unit of work, got %T: %v", err, err)
}

// TestSQLiteStore_CommitsWithCallerTransaction is the positive control: a store that always
// failed inside a unit of work would satisfy the rollback test on its own.
func TestSQLiteStore_CommitsWithCallerTransaction(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)

	w, err := wallet.New(f.tc.OrgID, f.tc.ProjectID, wallet.Params{
		Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR,
	})
	require.NoError(t, err)

	require.NoError(t, db.RunInTx(ctx, f.pool, func(ctx context.Context) error {
		return f.store.Save(ctx, w)
	}))

	got, err := f.store.ByID(f.ctx(t), w.ID)
	require.NoError(t, err)
	assert.Equal(t, "BCA", got.Name)
}

// TestSQLiteStore_ReadsEnrollInCallerTransaction pins that reads use the caller's transaction too,
// so a workflow can read back what it just wrote before the unit of work commits.
func TestSQLiteStore_ReadsEnrollInCallerTransaction(t *testing.T) {
	f := newSQLiteFixture(t)
	ctx := f.ctx(t)

	w, err := wallet.New(f.tc.OrgID, f.tc.ProjectID, wallet.Params{
		Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR,
	})
	require.NoError(t, err)

	require.NoError(t, db.RunInTx(ctx, f.pool, func(ctx context.Context) error {
		if sErr := f.store.Save(ctx, w); sErr != nil {
			return sErr
		}
		got, rErr := f.store.ByID(ctx, w.ID)
		if rErr != nil {
			return rErr
		}
		assert.Equal(t, "BCA", got.Name)

		list, lErr := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, wallet.ListOpts{})
		if lErr != nil {
			return lErr
		}
		assert.Len(t, list, 1, "List must see the uncommitted row through the caller's transaction")
		return nil
	}))
}
