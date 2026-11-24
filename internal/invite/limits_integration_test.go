//go:build integration

package invite_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/schema"
)

type oneMember struct{}

func (oneMember) CountMembers(context.Context, uuid.UUID) (int64, error)    { return 1, nil }
func (oneMember) CountOrgsJoined(context.Context, uuid.UUID) (int64, error) { return 0, nil }
func (oneMember) OrgName(context.Context, uuid.UUID) (string, error)        { return "Acme", nil }
func (oneMember) IsStaff(context.Context, uuid.UUID) (bool, error)          { return false, nil }

type slowPending struct{ invite.Store }

func (s slowPending) ListPending(ctx context.Context, orgID uuid.UUID) ([]*invite.Invite, error) {
	out, err := s.Store.ListPending(ctx, orgID)
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
	userID, orgID := seedInviteUserAndOrg(t, sqlDB, cfg.DB.TablePrefix)

	dbCfg := db.DBConfig{Driver: db.DriverPostgres, Schema: h.Schema, TablePrefix: cfg.DB.TablePrefix}
	pool := db.Pool{W: sqlDB, R: sqlDB}
	pc := tenant.NewPgConn(sqlDB)
	store := slowPending{invite.NewStore(dbCfg, pool, pc)}
	unex := 0
	wf := invite.NewSendWorkflow(store, &recordingMailer{}, "https://yasaku.example.test", newTestLogger(), noopUnexpected(&unex),
		invite.WithSendUnitOfWork(tenant.NewUnitOfWork(dbCfg, pool, pc)),
		invite.WithSendLimits(limits.NewService(limits.NewStore(dbCfg, pool), config.ModeCloud), oneMember{}))
	ctx := fakes.Owner(t.Context(), tenant.Context{OrgID: orgID, UserID: userID})
	for i := range 3 {
		_, err := wf.Execute(ctx, invite.SendRequest{Email: fmt.Sprintf("seed-%d@example.com", i), Role: invite.RoleMember})
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
			_, errs[i] = wf.Execute(ctx, invite.SendRequest{Email: fmt.Sprintf("race-%d@example.com", i), Role: invite.RoleMember})
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
	require.Equal(t, 1, won, "one member plus four pending fill org.members=5")
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
