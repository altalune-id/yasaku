//go:build integration

package project_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

type slowList struct{ project.Store }

func (s slowList) List(ctx context.Context, orgID uuid.UUID) ([]*project.Project, error) {
	out, err := s.Store.List(ctx, orgID)
	time.Sleep(50 * time.Millisecond)
	return out, err
}

func TestConcurrentCreates_AtLimitMinusOne(t *testing.T) {
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	cfg := config.Defaults()
	cfg.DB.Driver = "postgres"
	cfg.DB.DSN = h.DSN
	cfg.DB.Schema = h.Schema
	cfg.DB.AllowBypassRLS = true
	require.NoError(t, schema.MigrateUp(t.Context(), sqlDB, cfg))
	userID, orgID := seedUserAndOrg(t, sqlDB, cfg.DB.TablePrefix)

	dbCfg := db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}
	pool := db.Pool{W: sqlDB, R: sqlDB}
	pc := tenant.NewPgConn(sqlDB)
	unexpected := func(_ context.Context, msg string, cause error, _ ...any) *apperror.AppError {
		return apperror.New("yasaku.unexpected", msg, 0).WithCause(cause)
	}
	svc := project.NewService(slowList{project.NewStore(dbCfg, pool, pc)}, slog.New(slog.NewTextHandler(io.Discard, nil)), unexpected, tenant.NewUnitOfWork(dbCfg, pool, pc),
		project.WithLimits(limits.NewService(limits.NewStore(dbCfg, pool), config.ModeCloud), noOrgNames{}))
	ctx := fakes.Owner(t.Context(), tenant.Context{OrgID: orgID, UserID: userID})
	for i := range 2 {
		_, err := svc.Create(ctx, orgID, fmt.Sprintf("seed-%d", i), "Seed")
		require.NoError(t, err)
	}

	const racers = 16
	warmPool(t, sqlDB, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, racers)
	for i := range racers {
		wg.Go(func() {
			<-start
			_, errs[i] = svc.Create(ctx, orgID, fmt.Sprintf("race-%d", i), "Race")
		})
	}
	close(start)
	wg.Wait()

	won := 0
	for _, err := range errs {
		if err == nil {
			won++
			continue
		}
		require.True(t, limits.IsReachedError(err), "a loser must see the limit, got %T: %v", err, err)
	}
	t.Logf("won=%d errs=%v", won, errs)
	require.Equal(t, 1, won, "exactly one create fits under org.projects=3")
	all, err := svc.List(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, all, 3)
}

func warmPool(t *testing.T, sqlDB *sql.DB, n int) {
	t.Helper()
	sqlDB.SetMaxIdleConns(n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() { _, _ = sqlDB.ExecContext(t.Context(), "SELECT pg_sleep(0.2)") })
	}
	wg.Wait()
}

type noOrgNames struct{}

func (noOrgNames) OrgName(context.Context, uuid.UUID) (string, error) { return "", nil }
