package wallet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

type recordedOpening struct {
	walletID uuid.UUID
	amount   money.Amount
	at       time.Time
	by       uuid.UUID
}

type fakeRecorder struct {
	calls []recordedOpening
	err   error
}

func (r *fakeRecorder) RecordOpening(_ context.Context, walletID uuid.UUID, amount money.Amount, at time.Time, by uuid.UUID) error {
	r.calls = append(r.calls, recordedOpening{walletID: walletID, amount: amount, at: at, by: by})
	return r.err
}

type fakeUoW struct {
	calls     int
	sawInner  bool
	innerFail bool
}

func (u *fakeUoW) run(ctx context.Context, fn func(ctx context.Context) error) error {
	u.calls++
	err := fn(ctx)
	u.sawInner = true
	u.innerFail = err != nil
	return err
}

func newOpenWorkflow(t *testing.T) (*wallet.OpenWorkflow, *fakeRecorder, *fakeUoW, context.Context, tenant.Context, *counter) {
	t.Helper()
	svc, unex := newSvc(t, fakes.NewWallet())
	rec := &fakeRecorder{}
	uow := &fakeUoW{}
	ctx, tc := svcCtx(t)
	return wallet.NewOpenWorkflow(svc, rec, uow.run, testLogger(), countingUnexpected(unex)), rec, uow, ctx, tc, unex
}

func TestOpenWorkflow_Run(t *testing.T) {
	at := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	p := wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR}

	t.Run("records the opening balance inside the unit of work", func(t *testing.T) {
		wf, rec, uow, ctx, tc, unex := newOpenWorkflow(t)
		opening := money.New(100000, money.IDR)

		got, err := wf.Run(ctx, p, &opening, at)
		require.NoError(t, err)
		assert.Equal(t, "BCA", got.Name)
		assert.Equal(t, 1, uow.calls, "the write must run inside the injected unit of work")
		assert.False(t, uow.innerFail)

		require.Len(t, rec.calls, 1)
		assert.Equal(t, got.ID, rec.calls[0].walletID)
		assert.Equal(t, opening, rec.calls[0].amount)
		assert.Equal(t, at, rec.calls[0].at)
		assert.Equal(t, tc.UserID, rec.calls[0].by)
		assert.Zero(t, unex.n)
	})

	t.Run("a nil opening skips the recorder", func(t *testing.T) {
		wf, rec, uow, ctx, _, _ := newOpenWorkflow(t)

		got, err := wf.Run(ctx, p, nil, at)
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, rec.calls)
		assert.Equal(t, 1, uow.calls)
	})

	t.Run("a zero opening skips the recorder", func(t *testing.T) {
		wf, rec, _, ctx, _, _ := newOpenWorkflow(t)
		zero := money.Zero(money.IDR)

		_, err := wf.Run(ctx, p, &zero, at)
		require.NoError(t, err)
		assert.Empty(t, rec.calls)
	})

	t.Run("a recorder failure is returned and the unit of work sees it", func(t *testing.T) {
		wf, rec, uow, ctx, _, _ := newOpenWorkflow(t)
		boom := errors.New("ledger unavailable")
		rec.err = boom
		opening := money.New(100000, money.IDR)

		got, err := wf.Run(ctx, p, &opening, at)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.ErrorIs(t, err, boom)
		assert.True(t, uow.sawInner)
		assert.True(t, uow.innerFail, "the unit of work must see the error so the real RunInTx rolls back")
	})

	t.Run("a wallet validation failure never reaches the recorder", func(t *testing.T) {
		wf, rec, uow, ctx, _, unex := newOpenWorkflow(t)
		opening := money.New(100000, money.IDR)
		bad := wallet.Params{Name: "   ", Kind: wallet.KindBank, Currency: money.IDR}

		_, err := wf.Run(ctx, bad, &opening, at)
		assert.True(t, wallet.IsInvalidNameError(err), "got %T: %v", err, err)
		assert.Empty(t, rec.calls)
		assert.True(t, uow.innerFail)
		assert.Zero(t, unex.n)
	})

	t.Run("a unit-of-work failure is reported as unexpected", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		rec := &fakeRecorder{}
		uow := func(context.Context, func(context.Context) error) error { return errors.New("commit failed") }
		wf := wallet.NewOpenWorkflow(svc, rec, uow, testLogger(), countingUnexpected(unex))
		ctx, _ := svcCtx(t)

		_, err := wf.Run(ctx, p, nil, at)
		require.Error(t, err)
		assert.Equal(t, 1, unex.n)
	})

	t.Run("without a tenant context it refuses before opening the unit of work", func(t *testing.T) {
		wf, _, uow, _, _, _ := newOpenWorkflow(t)
		_, err := wf.Run(t.Context(), p, nil, at)
		require.Error(t, err)
		assert.Zero(t, uow.calls)
	})
}
