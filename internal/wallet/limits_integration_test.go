//go:build integration

package wallet_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

type slowList struct{ wallet.Store }

func (s slowList) List(ctx context.Context, orgID, projectID uuid.UUID, opts wallet.ListOpts) ([]*wallet.Wallet, error) {
	out, err := s.Store.List(ctx, orgID, projectID, opts)
	time.Sleep(50 * time.Millisecond)
	return out, err
}

func TestConcurrentCreates_AtLimitMinusOne(t *testing.T) {
	f := newPgFixture(t)
	dbCfg := db.DBConfig{Driver: db.DriverPostgres, Schema: f.schema, TablePrefix: f.prefix}
	pool := db.Pool{W: f.db, R: f.db}
	lstore := limits.NewStore(dbCfg, pool)
	ctx := fakes.Owner(t.Context(), f.tc)
	require.NoError(t, lstore.SetOverride(ctx, limits.Subject{Kind: limits.SubjectProject, ID: f.tc.ProjectID}, limits.ProjectWallets, 3, f.tc.UserID))
	svc := wallet.NewService(slowList{f.store}, testLogger(), countingUnexpected(&counter{}),
		wallet.WithMirror(nil, wallet.UnitOfWork(tenant.NewUnitOfWork(dbCfg, pool, tenant.NewPgConn(f.db)))),
		wallet.WithLimits(limits.NewService(lstore, config.ModeCloud)))
	for i := range 2 {
		_, err := svc.Create(ctx, wallet.Params{Name: fmt.Sprintf("seed-%d", i), Kind: wallet.KindBank, Currency: money.IDR})
		require.NoError(t, err)
	}

	const racers = 16
	warmPool(t, f.db, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, racers)
	for i := range racers {
		wg.Go(func() {
			<-start
			_, errs[i] = svc.Create(ctx, wallet.Params{Name: fmt.Sprintf("race-%d", i), Kind: wallet.KindBank, Currency: money.IDR})
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
	require.Equal(t, 1, won, "exactly one wallet fits under project.wallets=3")
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
