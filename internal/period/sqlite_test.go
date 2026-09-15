package period_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

func newSQLiteDB(t *testing.T) (*sql.DB, *config.Config) {
	t.Helper()
	cfg := config.Defaults()
	cfg.DB.Driver = db.DriverSQLite
	cfg.DB.DSN = filepath.Join(t.TempDir(), "period.db")

	sqlDB, err := db.Open(t.Context(), cfg.DB, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	return sqlDB, cfg
}

func seedTenant(t *testing.T, sqlDB *sql.DB, prefix string) tenant.Context {
	t.Helper()
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	now := sqliteent.SQLiteTime(time.Now())
	_, err := sqlDB.Exec(
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES (?, ?, '', '', 0, ?, ?)",
		tc.UserID.String(), tc.UserID.String()+"@x.com", now, now)
	require.NoError(t, err)
	_, err = sqlDB.Exec(
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, 'Org', ?, ?, ?)",
		tc.OrgID.String(), tc.OrgID.String()[:8], tc.UserID.String(), now, now)
	require.NoError(t, err)
	_, err = sqlDB.Exec(
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES (?, ?, ?, 'Web', ?, ?, ?)",
		tc.ProjectID.String(), tc.OrgID.String(), tc.ProjectID.String()[:8], tc.UserID.String(), now, now)
	require.NoError(t, err)
	return tc
}

func newSQLiteStoreForTest(t *testing.T) (period.Store, tenant.Context) {
	t.Helper()
	sqlDB, cfg := newSQLiteDB(t)
	tc := seedTenant(t, sqlDB, cfg.DB.TablePrefix)
	store := period.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix},
		db.Pool{W: sqlDB, R: sqlDB}, nil)
	return store, tc
}

func sampleSnapshot() period.Snapshot {
	return period.Snapshot{
		Currency: money.IDR,
		Income:   9_000_000,
		Expense:  2_500_000,
		Net:      6_500_000,
		TxCount:  12,
		Wallets: []period.WalletClosing{
			{WalletID: uuid.MustParse("6b3f2f2a-1f4f-4d8e-9a2b-0b1c2d3e4f50"), Name: "BCA", Closing: 6_500_000},
		},
		ComputedAt: time.Date(2026, 9, 24, 17, 0, 0, 0, time.UTC),
	}
}

func seedPeriod(ctx context.Context, t *testing.T, store period.Store, tc tenant.Context, start civil.Date, end *civil.Date) *period.Period {
	t.Helper()
	p, err := period.New(tc.OrgID, tc.ProjectID, start, "")
	require.NoError(t, err)
	if end != nil {
		p.EndDate = end
		p.Status = period.StatusClosed
		closedAt := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
		p.ClosedAt = &closedAt
		snap := sampleSnapshot()
		p.Snapshot = &snap
	}
	require.NoError(t, store.Save(ctx, p))
	return p
}

func TestSQLiteStore_RoundTripsOpenAndClosedPeriods(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	open := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, nil)
	got, err := store.ByID(ctx, open.ID)
	require.NoError(t, err)
	assert.Equal(t, open.StartDate, got.StartDate)
	assert.Nil(t, got.EndDate)
	assert.Nil(t, got.ClosedAt)
	assert.Nil(t, got.Snapshot)
	assert.Equal(t, period.StatusOpen, got.Status)
	assert.Equal(t, "Aug 2026", got.Name)

	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	open.EndDate = &end
	open.Status = period.StatusClosed
	closedAt := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	open.ClosedAt = &closedAt
	snap := sampleSnapshot()
	open.Snapshot = &snap
	require.NoError(t, store.Save(ctx, open))

	got, err = store.ByID(ctx, open.ID)
	require.NoError(t, err)
	require.NotNil(t, got.EndDate)
	assert.Equal(t, end, *got.EndDate)
	require.NotNil(t, got.ClosedAt)
	assert.True(t, closedAt.Equal(*got.ClosedAt))
	require.NotNil(t, got.Snapshot)
	assert.Equal(t, snap.Net, got.Snapshot.Net)
	assert.Equal(t, money.IDR, got.Snapshot.Currency)
	require.Len(t, got.Snapshot.Wallets, 1)
	assert.Equal(t, "BCA", got.Snapshot.Wallets[0].Name)
	assert.True(t, snap.ComputedAt.Equal(got.Snapshot.ComputedAt))
}

func TestSQLiteStore_Current(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	_, err := store.Current(ctx, tc.OrgID, tc.ProjectID)
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)

	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, &end)
	cur := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 9, Day: 25}, nil)

	got, err := store.Current(ctx, tc.OrgID, tc.ProjectID)
	require.NoError(t, err)
	assert.Equal(t, cur.ID, got.ID)
}

func TestSQLiteStore_SaveRejectsASecondCurrentPeriod(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, nil)
	second, err := period.New(tc.OrgID, tc.ProjectID, civil.Date{Year: 2026, Month: 9, Day: 25}, "")
	require.NoError(t, err)

	err = store.Save(ctx, second)
	assert.True(t, period.IsOverlapError(err), "got %T: %v", err, err)
}

func TestSQLiteStore_ContainingBoundaries(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	first := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, &end)
	cur := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 9, Day: 25}, nil)

	tests := []struct {
		name string
		day  civil.Date
		want *uuid.UUID
	}{
		{"first day of first period", civil.Date{Year: 2026, Month: 8, Day: 25}, &first.ID},
		{"last day of first period", end, &first.ID},
		{"first day of current", civil.Date{Year: 2026, Month: 9, Day: 25}, &cur.ID},
		{"far future is still current", civil.Date{Year: 2030, Month: 1, Day: 1}, &cur.ID},
		{"day before the first period", civil.Date{Year: 2026, Month: 8, Day: 24}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.Containing(ctx, tc.OrgID, tc.ProjectID, tt.day)
			if tt.want == nil {
				assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, *tt.want, got.ID)
		})
	}
}

func TestSQLiteStore_Neighbors(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	e1 := civil.Date{Year: 2026, Month: 9, Day: 24}
	e2 := civil.Date{Year: 2026, Month: 10, Day: 24}
	p1 := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, &e1)
	p2 := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 9, Day: 25}, &e2)
	p3 := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 10, Day: 25}, nil)

	prev, next, err := store.Neighbors(ctx, tc.OrgID, tc.ProjectID, p2.ID)
	require.NoError(t, err)
	require.NotNil(t, prev)
	require.NotNil(t, next)
	assert.Equal(t, p1.ID, prev.ID)
	assert.Equal(t, p3.ID, next.ID)

	prev, next, err = store.Neighbors(ctx, tc.OrgID, tc.ProjectID, p1.ID)
	require.NoError(t, err)
	assert.Nil(t, prev)
	require.NotNil(t, next)
	assert.Equal(t, p2.ID, next.ID)

	prev, next, err = store.Neighbors(ctx, tc.OrgID, tc.ProjectID, p3.ID)
	require.NoError(t, err)
	require.NotNil(t, prev)
	assert.Equal(t, p2.ID, prev.ID)
	assert.Nil(t, next)

	_, _, err = store.Neighbors(ctx, tc.OrgID, tc.ProjectID, uuid.New())
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)
}

func TestSQLiteStore_ListOrderAndOpts(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	e1 := civil.Date{Year: 2026, Month: 9, Day: 24}
	e2 := civil.Date{Year: 2026, Month: 10, Day: 24}
	p1 := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, &e1)
	p2 := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 9, Day: 25}, &e2)
	p3 := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 10, Day: 25}, nil)

	all, err := store.List(ctx, tc.OrgID, tc.ProjectID, period.ListOpts{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []uuid.UUID{p3.ID, p2.ID, p1.ID}, []uuid.UUID{all[0].ID, all[1].ID, all[2].ID})

	limited, err := store.List(ctx, tc.OrgID, tc.ProjectID, period.ListOpts{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, limited, 2)

	before := civil.Date{Year: 2026, Month: 9, Day: 25}
	older, err := store.List(ctx, tc.OrgID, tc.ProjectID, period.ListOpts{Before: &before})
	require.NoError(t, err)
	require.Len(t, older, 1)
	assert.Equal(t, p1.ID, older[0].ID)
}

func TestSQLiteStore_Closings(t *testing.T) {
	store, tc := newSQLiteStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	p := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, &end)

	first := &period.Closing{
		ID: uuid.Must(uuid.NewV7()), OrgID: tc.OrgID, ProjectID: tc.ProjectID, PeriodID: p.ID,
		ClosedAt: time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC), ClosedBy: tc.UserID, Snapshot: sampleSnapshot(),
	}
	require.NoError(t, store.SaveClosing(ctx, first))

	second := *first
	second.ID = uuid.Must(uuid.NewV7())
	second.ClosedAt = time.Date(2026, 9, 26, 3, 0, 0, 0, time.UTC)
	second.Snapshot.Net = 7_000_000
	require.NoError(t, store.SaveClosing(ctx, &second))

	got, err := store.ListClosings(ctx, tc.OrgID, tc.ProjectID, p.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, second.ID, got[0].ID, "closings come back newest first")
	assert.Equal(t, int64(7_000_000), got[0].Snapshot.Net)
	assert.Equal(t, int64(6_500_000), got[1].Snapshot.Net)
	assert.Equal(t, tc.UserID, got[0].ClosedBy)
}

// failingClosingStore fails the second of Close's three writes.
type failingClosingStore struct {
	period.Store
	err error
}

func (f *failingClosingStore) SaveClosing(context.Context, *period.Closing) error { return f.err }

func newCloseFixture(t *testing.T, wrap func(period.Store) period.Store) (*period.Service, period.Store, tenant.Context, context.Context) {
	t.Helper()
	sqlDB, cfg := newSQLiteDB(t)
	tc := seedTenant(t, sqlDB, cfg.DB.TablePrefix)
	pool := db.Pool{W: sqlDB, R: sqlDB}
	store := period.NewStore(
		db.DBConfig{Driver: db.DriverSQLite, TablePrefix: cfg.DB.TablePrefix}, pool, nil)

	svcStore := store
	if wrap != nil {
		svcStore = wrap(store)
	}
	loc := jakarta(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := period.NewService(
		svcStore, log, apperror.NewReporter(log, false).Unexpected,
		&fakeSettings{loc: loc, startDay: 25},
		&fakeSnapshotter{snap: sampleSnapshot()},
		func(ctx context.Context, fn func(ctx context.Context) error) error {
			return db.RunInTx(ctx, pool, fn)
		},
		func() time.Time { return time.Date(2026, 9, 25, 10, 0, 0, 0, loc) },
	)
	return svc, store, tc, tenant.Into(t.Context(), tc)
}

// NOTE: the control for the rollback test below, which would otherwise pass if Close never reached the store at all.
func TestSQLiteStore_CloseCommitsAllThreeWrites(t *testing.T) {
	svc, store, tc, ctx := newCloseFixture(t, nil)
	p := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, nil)

	_, err := svc.Close(ctx, p.ID, civil.Date{Year: 2026, Month: 9, Day: 24}, tc.UserID)
	require.NoError(t, err)

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, period.StatusClosed, got.Status)
	require.NotNil(t, got.Snapshot)

	closings, err := store.ListClosings(ctx, tc.OrgID, tc.ProjectID, p.ID)
	require.NoError(t, err)
	assert.Len(t, closings, 1)

	all, err := store.List(ctx, tc.OrgID, tc.ProjectID, period.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 2, "the successor period must be committed alongside the closing")
}

// TestSQLiteStore_CloseRollsBackEveryWriteOnFailure proves the SQLite store enrols in the caller's unit of work.
func TestSQLiteStore_CloseRollsBackEveryWriteOnFailure(t *testing.T) {
	boom := errors.New("period closing write failed")
	svc, store, tc, ctx := newCloseFixture(t, func(inner period.Store) period.Store {
		return &failingClosingStore{Store: inner, err: boom}
	})
	p := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, nil)

	_, err := svc.Close(ctx, p.ID, civil.Date{Year: 2026, Month: 9, Day: 24}, tc.UserID)
	require.Error(t, err)

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.True(t, got.IsCurrent(), "the period's end date survived a rolled-back close")
	assert.Equal(t, period.StatusOpen, got.Status, "the period was left closed by a rolled-back close")
	assert.Nil(t, got.Snapshot, "a snapshot survived a rolled-back close")
	assert.Nil(t, got.ClosedAt)

	closings, err := store.ListClosings(ctx, tc.OrgID, tc.ProjectID, p.ID)
	require.NoError(t, err)
	assert.Empty(t, closings)

	all, err := store.List(ctx, tc.OrgID, tc.ProjectID, period.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1, "a successor period survived a rolled-back close")
}
