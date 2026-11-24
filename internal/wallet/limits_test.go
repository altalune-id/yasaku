package wallet_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

func limitedWallets(t *testing.T, limit int64) (*wallet.Service, *fakes.Wallet, context.Context, tenant.Context) {
	t.Helper()
	ctx, tc := svcCtx(t)
	store := fakes.NewLimits()
	require.NoError(t, store.SetOverride(ctx, limits.Subject{Kind: limits.SubjectProject, ID: tc.ProjectID}, limits.ProjectWallets, limit, tc.UserID))
	ws := fakes.NewWallet()
	c := &counter{}
	svc := wallet.NewService(ws, testLogger(), countingUnexpected(c),
		wallet.WithMirror(nil, wallet.UnitOfWork(fakes.UnitOfWork)), wallet.WithLimits(limits.NewService(store, config.ModeCloud)))
	return svc, ws, ctx, tc
}

func TestWalletCreate_AtLimitRefuses(t *testing.T) {
	svc, _, ctx, _ := limitedWallets(t, 1)
	mustCreate(ctx, t, svc, "BCA")
	_, err := svc.Create(ctx, wallet.Params{Name: "Mandiri", Kind: wallet.KindBank, Currency: money.IDR})
	require.True(t, limits.IsReachedError(err), "got %T: %v", err, err)
}

func TestWalletCreate_ArchivedDoNotCount(t *testing.T) {
	svc, _, ctx, _ := limitedWallets(t, 1)
	w := mustCreate(ctx, t, svc, "BCA")
	_, err := svc.Archive(ctx, w.ID)
	require.NoError(t, err)
	mustCreate(ctx, t, svc, "Mandiri")
}

func TestOpenWallet_AtLimitRefuses(t *testing.T) {
	svc, _, ctx, _ := limitedWallets(t, 1)
	mustCreate(ctx, t, svc, "BCA")
	wf := wallet.NewOpenWorkflow(svc, &fakeRecorder{}, &fakeDater{}, fakes.UnitOfWork, testLogger(), countingUnexpected(&counter{}))
	_, _, err := wf.Run(ctx, wallet.Params{Name: "Cash", Kind: wallet.KindCash, Currency: money.IDR}, nil, time.Now())
	require.True(t, limits.IsReachedError(err), "got %T: %v", err, err)
}

func TestUnarchive_AtLimitRefuses(t *testing.T) {
	svc, _, ctx, _ := limitedWallets(t, 1)
	old := mustCreate(ctx, t, svc, "BCA")
	_, err := svc.Archive(ctx, old.ID)
	require.NoError(t, err)
	mustCreate(ctx, t, svc, "Mandiri")
	_, err = svc.Unarchive(ctx, old.ID)
	require.True(t, limits.IsReachedError(err), "got %T: %v", err, err)
}
