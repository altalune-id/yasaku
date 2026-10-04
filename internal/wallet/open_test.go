package wallet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
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
	calls    []recordedOpening
	err      error
	onRecord func()
}

func (r *fakeRecorder) RecordOpening(_ context.Context, walletID uuid.UUID, amount money.Amount, at time.Time, by uuid.UUID) (uuid.UUID, error) {
	r.calls = append(r.calls, recordedOpening{walletID: walletID, amount: amount, at: at, by: by})
	if r.onRecord != nil {
		r.onRecord()
	}
	if r.err != nil {
		return uuid.Nil, r.err
	}
	return uuid.New(), nil
}

type fakeDater struct {
	moveTo *time.Time
	calls  int
	err    error
}

func (d *fakeDater) OpeningDate(_ context.Context, at time.Time) (wallet.OpeningDate, error) {
	d.calls++
	if d.err != nil {
		return wallet.OpeningDate{}, d.err
	}
	if d.moveTo == nil {
		return wallet.OpeningDate{At: at, Date: civil.DateOf(at, time.UTC)}, nil
	}
	return wallet.OpeningDate{At: *d.moveTo, Date: civil.DateOf(*d.moveTo, time.UTC), Moved: true}, nil
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
	wf, rec, uow, _, ctx, tc, unex := newOpenWorkflowDated(t)
	return wf, rec, uow, ctx, tc, unex
}

func newOpenWorkflowDated(t *testing.T) (*wallet.OpenWorkflow, *fakeRecorder, *fakeUoW, *fakeDater, context.Context, tenant.Context, *counter) {
	t.Helper()
	svc, unex := newSvc(t, fakes.NewWallet())
	rec := &fakeRecorder{}
	uow := &fakeUoW{}
	dater := &fakeDater{}
	ctx, tc := svcCtx(t)
	return wallet.NewOpenWorkflow(svc, rec, dater, uow.run, testLogger(), countingUnexpected(unex)), rec, uow, dater, ctx, tc, unex
}

func TestOpenWorkflow_Run(t *testing.T) {
	at := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	p := wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR}

	t.Run("records the opening balance inside the unit of work", func(t *testing.T) {
		wf, rec, uow, ctx, tc, unex := newOpenWorkflow(t)
		opening := money.New(100000, money.IDR)

		got, _, err := wf.Run(ctx, p, &opening, at)
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

		got, _, err := wf.Run(ctx, p, nil, at)
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, rec.calls)
		assert.Equal(t, 1, uow.calls)
	})

	t.Run("a zero opening skips the recorder", func(t *testing.T) {
		wf, rec, _, ctx, _, _ := newOpenWorkflow(t)
		zero := money.Zero(money.IDR)

		_, _, err := wf.Run(ctx, p, &zero, at)
		require.NoError(t, err)
		assert.Empty(t, rec.calls)
	})

	t.Run("a recorder failure is returned and the unit of work sees it", func(t *testing.T) {
		wf, rec, uow, ctx, _, _ := newOpenWorkflow(t)
		boom := errors.New("ledger unavailable")
		rec.err = boom
		opening := money.New(100000, money.IDR)

		got, _, err := wf.Run(ctx, p, &opening, at)
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

		_, _, err := wf.Run(ctx, bad, &opening, at)
		assert.True(t, wallet.IsInvalidNameError(err), "got %T: %v", err, err)
		assert.Empty(t, rec.calls)
		assert.True(t, uow.innerFail)
		assert.Zero(t, unex.n)
	})

	t.Run("a unit-of-work failure is reported as unexpected", func(t *testing.T) {
		svc, unex := newSvc(t, fakes.NewWallet())
		rec := &fakeRecorder{}
		uow := func(context.Context, func(context.Context) error) error { return errors.New("commit failed") }
		wf := wallet.NewOpenWorkflow(svc, rec, &fakeDater{}, uow, testLogger(), countingUnexpected(unex))
		ctx, _ := svcCtx(t)

		_, _, err := wf.Run(ctx, p, nil, at)
		require.Error(t, err)
		assert.Equal(t, 1, unex.n)
	})

	t.Run("without a tenant context it refuses before opening the unit of work", func(t *testing.T) {
		wf, _, uow, _, _, _ := newOpenWorkflow(t)
		_, _, err := wf.Run(t.Context(), p, nil, at)
		require.Error(t, err)
		assert.Zero(t, uow.calls)
	})
}

func TestOpenWorkflow_DatesTheOpeningBalanceItself(t *testing.T) {
	at := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	p := wallet.Params{Name: "Jago", Kind: wallet.KindBank, Currency: money.IDR}
	opening := money.New(250000, money.IDR)

	t.Run("a closed period moves the opening balance, whatever instant the surface passed", func(t *testing.T) {
		wf, rec, uow, dater, ctx, _, _ := newOpenWorkflowDated(t)
		next := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)
		dater.moveTo = &next

		got, dated, err := wf.Run(ctx, p, &opening, at)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, dated.Moved)
		assert.Equal(t, next, dated.At)
		require.Len(t, rec.calls, 1)
		assert.Equal(t, next, rec.calls[0].at, "the recorder only ever sees the dated instant")
		assert.Equal(t, 1, uow.calls, "the date is read inside the same unit of work as the writes")
	})

	t.Run("an open period keeps the instant", func(t *testing.T) {
		wf, rec, _, dater, ctx, _, _ := newOpenWorkflowDated(t)

		_, dated, err := wf.Run(ctx, p, &opening, at)
		require.NoError(t, err)
		assert.False(t, dated.Moved)
		assert.Equal(t, at, dated.At)
		require.Len(t, rec.calls, 1)
		assert.Equal(t, at, rec.calls[0].at)
		assert.Equal(t, 1, dater.calls)
	})

	t.Run("no opening balance asks for no date", func(t *testing.T) {
		wf, _, _, dater, ctx, _, _ := newOpenWorkflowDated(t)

		_, dated, err := wf.Run(ctx, p, nil, at)
		require.NoError(t, err)
		assert.False(t, dated.Moved)
		assert.Zero(t, dater.calls)
	})

	t.Run("a dating failure writes nothing", func(t *testing.T) {
		wf, rec, uow, dater, ctx, _, _ := newOpenWorkflowDated(t)
		dater.err = errors.New("periods unavailable")

		_, _, err := wf.Run(ctx, p, &opening, at)
		require.Error(t, err)
		assert.Empty(t, rec.calls)
		assert.True(t, uow.innerFail, "the unit of work must roll the wallet back")
	})

	t.Run("a preview dates the opening balance the same way and writes nothing", func(t *testing.T) {
		wf, rec, uow, dater, ctx, _, _ := newOpenWorkflowDated(t)
		next := time.Date(2026, 10, 2, 17, 0, 0, 0, time.UTC)
		dater.moveTo = &next

		dated, err := wf.Preview(ctx, &opening, at)
		require.NoError(t, err)
		assert.True(t, dated.Moved)
		assert.Equal(t, next, dated.At)
		assert.Empty(t, rec.calls)
		assert.Zero(t, uow.calls)
	})
}
