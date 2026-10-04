package transaction_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/transaction"
)

// NOTE: newMirroredFixture rebuilds the fixture's service on fakes.UnitOfWork, which puts a tx on ctx, so a test can tell a Mark inside the unit of work from one after it.
func newMirroredFixture(t *testing.T) (*fixture, *fakes.Mirror[transaction.MirrorRef]) {
	t.Helper()
	f := newFixture(t)
	m := &fakes.Mirror[transaction.MirrorRef]{}
	f.svc = transaction.NewService(f.store, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
			*f.unexpecteds++
			return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
				&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
		},
		transaction.WalletReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.WalletInfo, error) {
			w, ok := f.wallets[id]
			if !ok {
				return transaction.WalletInfo{}, &transaction.NotFoundError{ID: id.String()}
			}
			return w, nil
		}),
		transaction.CategoryReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.CategoryInfo, error) {
			return f.categories[id], nil
		}),
		f.periods, transaction.UnitOfWork(fakes.UnitOfWork), transaction.WithMirror(m))
	return f, m
}

func wantRefs(entries ...any) []transaction.MirrorRef {
	out := make([]transaction.MirrorRef, 0, len(entries)/2)
	for i := 0; i < len(entries); i += 2 {
		out = append(out, transaction.MirrorRef{Entity: entries[i].(string), ID: entries[i+1].(uuid.UUID)})
	}
	return out
}

func TestRecord_MarksInsideTheUnitOfWorkAndKicksAfterTheCommit(t *testing.T) {
	f, m := newMirroredFixture(t)
	tx, err := f.svc.Record(f.ctx, transaction.RecordInput{
		WalletID: f.walletIDR, Kind: transaction.KindExpense, Amount: idr(10_000), CategoryID: &f.expenseCat, OccurredAt: augustDay(3),
	})
	require.NoError(t, err)
	want := wantRefs(transaction.MirrorTransaction, tx.ID, transaction.MirrorWallet, f.walletIDR)
	require.Equal(t, []fakes.MirrorCall[transaction.MirrorRef]{{Refs: want, InTx: true}}, m.Marks())
	require.Equal(t, []fakes.MirrorCall[transaction.MirrorRef]{{Refs: want, InTx: false}}, m.Kicks())
}

func TestRecord_ATransferMarksBothWallets(t *testing.T) {
	f, m := newMirroredFixture(t)
	tx, err := f.svc.Record(f.ctx, transaction.RecordInput{
		WalletID: f.walletIDR, ToWalletID: &f.walletIDR2, Kind: transaction.KindTransfer, Amount: idr(10_000), OccurredAt: augustDay(3),
	})
	require.NoError(t, err)
	require.Equal(t, wantRefs(transaction.MirrorTransaction, tx.ID, transaction.MirrorWallet, f.walletIDR, transaction.MirrorWallet, f.walletIDR2),
		m.Kicks()[0].Refs)
}

func TestRecord_AFailedMarkFailsTheWriteAndKicksNothing(t *testing.T) {
	f, m := newMirroredFixture(t)
	m.MarkErr = errors.New("state table gone")
	_, err := f.svc.Record(f.ctx, transaction.RecordInput{
		WalletID: f.walletIDR, Kind: transaction.KindExpense, Amount: idr(10_000), OccurredAt: augustDay(3),
	})
	require.Error(t, err)
	require.Equal(t, 1, *f.unexpecteds, "a failed mark is reported once")
	require.Empty(t, m.Kicks())
}

func TestRecordBatch_KicksOnceForEveryRecordedRow(t *testing.T) {
	f, m := newMirroredFixture(t)
	res := f.svc.RecordBatch(f.ctx, []transaction.RecordInput{
		{WalletID: f.walletIDR, Kind: transaction.KindExpense, Amount: idr(1_000), OccurredAt: augustDay(3)},
		{WalletID: f.archived, Kind: transaction.KindExpense, Amount: idr(1_000), OccurredAt: augustDay(3)},
		{WalletID: f.walletIDR2, Kind: transaction.KindIncome, Amount: idr(2_000), OccurredAt: augustDay(4)},
	})
	require.NoError(t, res[0].Err)
	require.Error(t, res[1].Err)
	require.NoError(t, res[2].Err)
	require.Len(t, m.Marks(), 2, "one mark per recorded row, each in its own unit of work")
	require.Len(t, m.Kicks(), 1)
	require.Equal(t, wantRefs(
		transaction.MirrorTransaction, res[0].Transaction.ID, transaction.MirrorWallet, f.walletIDR,
		transaction.MirrorTransaction, res[2].Transaction.ID, transaction.MirrorWallet, f.walletIDR2,
	), m.Kicks()[0].Refs)
}

func TestRevise_MarksTheWalletsBeforeAndAfter(t *testing.T) {
	f, m := newMirroredFixture(t)
	tx := f.expense(t, augustDay(3), 10_000, "lunch")
	_, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{WalletID: &f.walletIDR2})
	require.NoError(t, err)
	last := m.Marks()[len(m.Marks())-1]
	require.True(t, last.InTx)
	require.Equal(t, wantRefs(transaction.MirrorTransaction, tx.ID, transaction.MirrorWallet, f.walletIDR2, transaction.MirrorWallet, tx.WalletID), last.Refs)
}

func TestDelete_MarksTheTransactionDeleted(t *testing.T) {
	f, m := newMirroredFixture(t)
	tx := f.expense(t, augustDay(3), 10_000, "lunch")
	require.NoError(t, f.svc.Delete(f.ctx, tx.ID))
	last := m.Kicks()[len(m.Kicks())-1]
	require.Equal(t, transaction.MirrorRef{Entity: transaction.MirrorTransaction, ID: tx.ID, Deleted: true}, last.Refs[0])
	require.Equal(t, transaction.MirrorRef{Entity: transaction.MirrorWallet, ID: tx.WalletID}, last.Refs[1])
}

func TestRecordOpening_MarksWithoutKicking(t *testing.T) {
	f, m := newMirroredFixture(t)
	id, err := f.svc.RecordOpening(f.ctx, f.walletIDR, idr(500_000), augustDay(1), f.tc.UserID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, id)
	require.Equal(t, wantRefs(transaction.MirrorTransaction, id, transaction.MirrorWallet, f.walletIDR), m.Marks()[0].Refs)
	require.Empty(t, m.Kicks(), "the caller's workflow kicks after its own commit")
}

func TestAdjust_MarksInsideItsUnitOfWorkAndKicksAfter(t *testing.T) {
	f, m := newMirroredFixture(t)
	tx, err := f.svc.Adjust(f.ctx, f.walletIDR, idr(150_000), augustDay(5), f.tc.UserID)
	require.NoError(t, err)
	want := wantRefs(transaction.MirrorTransaction, tx.ID, transaction.MirrorWallet, f.walletIDR)
	require.Equal(t, []fakes.MirrorCall[transaction.MirrorRef]{{Refs: want, InTx: true}}, m.Marks())
	require.Equal(t, []fakes.MirrorCall[transaction.MirrorRef]{{Refs: want, InTx: false}}, m.Kicks())
}
