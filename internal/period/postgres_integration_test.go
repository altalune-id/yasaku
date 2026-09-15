//go:build integration

package period_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/money"
	"altalune.id/yasaku/schema"
)

func newPostgresStoreForTest(t *testing.T) (period.Store, tenant.Context) {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	tc := seedPgTenant(t, sqlDB, cfg.DB.TablePrefix)
	pc := tenant.NewPgConn(sqlDB)
	store := period.NewStore(
		db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix},
		db.Pool{W: sqlDB, R: sqlDB}, pc)
	return store, tc
}

func seedPgTenant(t *testing.T, sqlDB *sql.DB, prefix string) tenant.Context {
	t.Helper()
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	now := time.Now().UTC()
	_, err := sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"users (id, email, name, avatar_url, is_admin, created_at, updated_at) VALUES ($1, $2, '', '', false, $3, $3)",
		tc.UserID, tc.UserID.String()+"@x.co", now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"orgs (id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, 'Acme', $3, $4, $4)",
		tc.OrgID, tc.OrgID.String()[:8], tc.UserID, now)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(t.Context(),
		"INSERT INTO "+prefix+"projects (id, org_id, slug, name, created_by, created_at, updated_at) VALUES ($1, $2, $3, 'Web', $4, $5, $5)",
		tc.ProjectID, tc.OrgID, tc.ProjectID.String()[:8], tc.UserID, now)
	require.NoError(t, err)
	return tc
}

func TestPostgres_Period_RoundTripsDatesAndSnapshot(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	p := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, nil)
	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, civil.Date{Year: 2026, Month: 8, Day: 25}, got.StartDate)
	assert.Nil(t, got.EndDate)
	assert.Nil(t, got.ClosedAt)
	assert.Nil(t, got.Snapshot)

	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	p.EndDate = &end
	p.Status = period.StatusClosed
	closedAt := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	p.ClosedAt = &closedAt
	snap := sampleSnapshot()
	p.Snapshot = &snap
	require.NoError(t, store.Save(ctx, p))

	got, err = store.ByID(ctx, p.ID)
	require.NoError(t, err)
	require.NotNil(t, got.EndDate)
	assert.Equal(t, end, *got.EndDate)
	require.NotNil(t, got.ClosedAt)
	assert.True(t, closedAt.Equal(*got.ClosedAt))
	require.NotNil(t, got.Snapshot)
	assert.Equal(t, money.IDR, got.Snapshot.Currency)
	assert.Equal(t, int64(6_500_000), got.Snapshot.Net)
	require.Len(t, got.Snapshot.Wallets, 1)
	assert.Equal(t, "BCA", got.Snapshot.Wallets[0].Name)
	assert.True(t, snap.ComputedAt.Equal(got.Snapshot.ComputedAt))
}

func TestPostgres_Period_SnapshotColumnIsRealJSONB(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	p := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, &end)

	got, err := store.ByID(ctx, p.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Snapshot)
	assert.Equal(t, 12, got.Snapshot.TxCount, "the JSONB document survives a jsonb round trip")
}

func TestPostgres_Period_Current(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
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

func TestPostgres_Period_SaveRejectsASecondCurrentPeriod(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, nil)
	second, err := period.New(tc.OrgID, tc.ProjectID, civil.Date{Year: 2026, Month: 9, Day: 25}, "")
	require.NoError(t, err)

	err = store.Save(ctx, second)
	assert.True(t, period.IsOverlapError(err), "got %T: %v", err, err)
}

func TestPostgres_Period_ContainingBoundaries(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	first := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, &end)
	cur := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 9, Day: 25}, nil)

	for _, tt := range []struct {
		name string
		day  civil.Date
		want *uuid.UUID
	}{
		{"first day", civil.Date{Year: 2026, Month: 8, Day: 25}, &first.ID},
		{"last day", end, &first.ID},
		{"next period starts", civil.Date{Year: 2026, Month: 9, Day: 25}, &cur.ID},
		{"before the first period", civil.Date{Year: 2026, Month: 8, Day: 24}, nil},
	} {
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

func TestPostgres_Period_Neighbors(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
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
}

func TestPostgres_Period_ListOrderAndOpts(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	e1 := civil.Date{Year: 2026, Month: 9, Day: 24}
	p1 := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 8, Day: 25}, &e1)
	p2 := seedPeriod(ctx, t, store, tc, civil.Date{Year: 2026, Month: 9, Day: 25}, nil)

	all, err := store.List(ctx, tc.OrgID, tc.ProjectID, period.ListOpts{})
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, p2.ID, all[0].ID)
	assert.Equal(t, p1.ID, all[1].ID)

	before := civil.Date{Year: 2026, Month: 9, Day: 25}
	older, err := store.List(ctx, tc.OrgID, tc.ProjectID, period.ListOpts{Before: &before})
	require.NoError(t, err)
	require.Len(t, older, 1)
	assert.Equal(t, p1.ID, older[0].ID)

	limited, err := store.List(ctx, tc.OrgID, tc.ProjectID, period.ListOpts{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, limited, 1)
}

func TestPostgres_Period_Closings(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
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
	assert.Equal(t, second.ID, got[0].ID)
	assert.Equal(t, int64(7_000_000), got[0].Snapshot.Net)
	assert.Equal(t, tc.UserID, got[1].ClosedBy)
}

func TestPostgres_Period_NotFound(t *testing.T) {
	store, tc := newPostgresStoreForTest(t)
	ctx := tenant.Into(t.Context(), tc)

	_, err := store.ByID(ctx, uuid.New())
	assert.True(t, period.IsNotFoundError(err), "got %T: %v", err, err)
}
