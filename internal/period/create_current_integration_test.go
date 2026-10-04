//go:build integration

package period_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"sync"
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
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

type pgRaceFixture struct {
	db    *sql.DB
	store period.Store
	uow   tenant.UnitOfWork
	tc    tenant.Context
}

func newPgRaceFixture(t *testing.T) pgRaceFixture {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)

	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))

	dbCfg := db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}
	pool := db.Pool{W: sqlDB, R: sqlDB}
	pc := tenant.NewPgConn(sqlDB)
	return pgRaceFixture{
		db:    sqlDB,
		store: period.NewStore(dbCfg, pool, pc),
		uow:   tenant.NewUnitOfWork(dbCfg, pool, pc),
		tc:    seedPgTenant(t, sqlDB, cfg.DB.TablePrefix),
	}
}

func TestPostgres_Period_CreateCurrentKeepsTheUnitOfWorkUsableAfterALostRace(t *testing.T) {
	f := newPgRaceFixture(t)
	ctx := tenant.Into(t.Context(), f.tc)
	first := seedPeriod(ctx, t, f.store, f.tc, civil.Date{Year: 2026, Month: 8, Day: 25}, nil)

	second, err := period.New(f.tc.OrgID, f.tc.ProjectID, civil.Date{Year: 2026, Month: 9, Day: 25}, "")
	require.NoError(t, err)
	var reread *period.Period
	require.NoError(t, f.uow(ctx, func(ctx context.Context) error {
		created, cErr := f.store.CreateCurrent(ctx, second)
		if cErr != nil {
			return cErr
		}
		assert.False(t, created, "a project with a current period keeps it")
		var rErr error
		reread, rErr = f.store.Current(ctx, f.tc.OrgID, f.tc.ProjectID)
		return rErr
	}), "the read after a lost create must run in the same transaction")
	assert.Equal(t, first.ID, reread.ID)
}

// NOTE: the second writer reads no current period, then blocks on the first writer's uncommitted row; the test releases the first only once Postgres reports the second waiting on that lock.
func TestPostgres_Period_TwoConcurrentEnsureCurrentInUnitsOfWorkShareTheFirstPeriod(t *testing.T) {
	f := newPgRaceFixture(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := period.NewService(f.store, log, apperror.NewReporter(log, false).Unexpected,
		&fakeSettings{loc: time.UTC, startDay: 1}, &fakeSnapshotter{}, period.UnitOfWork(f.uow),
		func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) })
	ctx, cancel := context.WithTimeout(tenant.Into(t.Context(), f.tc), 30*time.Second)
	defer cancel()

	var first, second period.Period
	var firstErr, secondErr error
	created := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		firstErr = f.uow(ctx, func(ctx context.Context) error {
			p, err := svc.EnsureCurrent(ctx)
			if err != nil {
				close(created)
				return err
			}
			first = *p
			close(created)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	})
	<-created
	loser := "period-race-" + uuid.NewString()[:8]
	wg.Go(func() {
		secondErr = f.uow(ctx, func(ctx context.Context) error {
			tx, _ := db.CurrentTx(ctx)
			if _, err := tx.ExecContext(ctx, "SELECT set_config('application_name', $1, true)", loser); err != nil {
				return err
			}
			p, err := svc.EnsureCurrent(ctx)
			if err != nil {
				return err
			}
			second = *p
			return nil
		})
	})

	waiting := assert.Eventually(t, func() bool {
		var n int
		err := f.db.QueryRowContext(ctx,
			"SELECT count(*) FROM pg_stat_activity WHERE application_name = $1 AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()",
			loser).Scan(&n)
		return err == nil && n > 0
	}, 10*time.Second, 20*time.Millisecond, "the second writer must block on the first writer's row")
	close(release)
	wg.Wait()
	require.True(t, waiting)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr, "the loser must re-read the winner's period in its still-usable transaction")
	assert.Equal(t, first.ID, second.ID)

	all, err := f.store.List(tenant.Into(t.Context(), f.tc), f.tc.OrgID, f.tc.ProjectID, period.ListOpts{})
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
