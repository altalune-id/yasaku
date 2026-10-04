package wallet_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

func newMirroredSvc(t *testing.T) (*wallet.Service, *fakes.Mirror[wallet.MirrorRef]) {
	t.Helper()
	m := &fakes.Mirror[wallet.MirrorRef]{}
	c := &counter{}
	return wallet.NewService(fakes.NewWallet(), testLogger(), countingUnexpected(c), wallet.WithMirror(m, fakes.UnitOfWork)), m
}

func TestWalletWrites_MarkInsideTheUnitOfWorkAndKickAfter(t *testing.T) {
	svc, m := newMirroredSvc(t)
	ctx, _ := svcCtx(t)
	w := mustCreate(ctx, t, svc, "BCA")
	ref := wallet.MirrorRef{Entity: wallet.MirrorWallet, ID: w.ID}
	require.Equal(t, []fakes.MirrorCall[wallet.MirrorRef]{{Refs: []wallet.MirrorRef{ref}, InTx: true}}, m.Marks())
	require.Equal(t, []fakes.MirrorCall[wallet.MirrorRef]{{Refs: []wallet.MirrorRef{ref}, InTx: false}}, m.Kicks())

	_, err := svc.Archive(ctx, w.ID)
	require.NoError(t, err)
	require.Equal(t, ref, m.Kicks()[1].Refs[0], "archive does not change the name, so it does not cascade")

	require.NoError(t, svc.Delete(ctx, w.ID))
	require.Equal(t, wallet.MirrorRef{Entity: wallet.MirrorWallet, ID: w.ID, Deleted: true}, m.Kicks()[2].Refs[0])
}

func TestWalletRename_CascadesOnlyWhenTheNameChanges(t *testing.T) {
	svc, m := newMirroredSvc(t)
	ctx, _ := svcCtx(t)
	w := mustCreate(ctx, t, svc, "BCA")
	_, err := svc.Rename(ctx, w.ID, "BCA Main")
	require.NoError(t, err)
	require.True(t, m.Kicks()[1].Refs[0].Cascade)
	_, err = svc.Rename(ctx, w.ID, "BCA Main")
	require.NoError(t, err)
	require.False(t, m.Kicks()[2].Refs[0].Cascade)
	_, err = svc.Edit(ctx, w.ID, "BCA Daily", wallet.KindBank, "", false)
	require.NoError(t, err)
	require.True(t, m.Kicks()[3].Refs[0].Cascade)
}

func TestOpenWorkflow_KicksTheWalletAndItsOpeningAfterTheCommit(t *testing.T) {
	svc, m := newMirroredSvc(t)
	rec := &fakeRecorder{}
	wf := wallet.NewOpenWorkflow(svc, rec, &fakeDater{}, wallet.UnitOfWork(fakes.UnitOfWork), testLogger(), countingUnexpected(&counter{}))
	ctx, _ := svcCtx(t)
	opening := money.New(100_000, money.IDR)
	got, _, err := wf.Run(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR}, &opening, time.Now())
	require.NoError(t, err)
	require.Len(t, m.Marks(), 1)
	require.True(t, m.Marks()[0].InTx, "the workflow marks the wallet inside its own unit of work")
	require.Len(t, m.Kicks(), 1)
	kick := m.Kicks()[0]
	require.False(t, kick.InTx)
	require.Equal(t, wallet.MirrorRef{Entity: wallet.MirrorWallet, ID: got.ID}, kick.Refs[0])
	require.Equal(t, wallet.MirrorTransaction, kick.Refs[1].Entity)
	require.NotEqual(t, uuid.Nil, kick.Refs[1].ID)
}

func TestWithMirror_PanicsAtWiringWithoutAUnitOfWork(t *testing.T) {
	require.PanicsWithValue(t, "wallet: WithMirror needs a unit of work, or a mark would not commit with its write", func() {
		wallet.WithMirror(&fakes.Mirror[wallet.MirrorRef]{}, nil)
	})
	require.NotPanics(t, func() { wallet.WithMirror(nil, nil) })
}

func TestOpenWorkflow_MarksTheWalletAfterTheOpeningTransaction(t *testing.T) {
	svc, m := newMirroredSvc(t)
	marksAtRecord := -1
	rec := &fakeRecorder{}
	rec.onRecord = func() { marksAtRecord = len(m.Marks()) }
	wf := wallet.NewOpenWorkflow(svc, rec, &fakeDater{}, wallet.UnitOfWork(fakes.UnitOfWork), testLogger(), countingUnexpected(&counter{}))
	ctx, _ := svcCtx(t)
	opening := money.New(100_000, money.IDR)
	_, _, err := wf.Run(ctx, wallet.Params{Name: "BCA", Kind: wallet.KindBank, Currency: money.IDR}, &opening, time.Now())
	require.NoError(t, err)
	require.Equal(t, 0, marksAtRecord, "the wallet is marked after RecordOpening marks its transaction, in lock order")
	require.Len(t, m.Marks(), 1)

	_, _, err = wf.Run(ctx, wallet.Params{Name: "Cash", Kind: wallet.KindCash, Currency: money.IDR}, nil, time.Now())
	require.NoError(t, err)
	require.Len(t, m.Marks(), 2, "a wallet with no opening balance is still marked")
}
