//go:build integration

package transaction_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

func newPgMirroredService(f pgFixture, m transaction.Mirror) *transaction.Service {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pc := tenant.NewPgConn(f.db)
	return transaction.NewService(f.store, log, apperror.NewReporter(log, false).Unexpected,
		transaction.WalletReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.WalletInfo, error) {
			return transaction.WalletInfo{ID: id, Currency: money.IDR}, nil
		}),
		transaction.CategoryReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.CategoryInfo, error) {
			return transaction.CategoryInfo{ID: id, Kind: "expense"}, nil
		}),
		&periodStub{
			infos:     map[uuid.UUID]transaction.PeriodInfo{f.period: {ID: f.period}},
			byInstant: func(time.Time) (uuid.UUID, bool) { return f.period, true },
		},
		transaction.UnitOfWork(func(ctx context.Context, fn func(ctx context.Context) error) error {
			return tenant.RunInTx(ctx, pc, f.tc, fn)
		}),
		transaction.WithMirror(m),
	)
}

func TestPostgres_Transaction_RecordReviseDeleteMarkInsideARealUnitOfWork(t *testing.T) {
	f := newPgFixture(t)
	m := &fakes.Mirror[transaction.MirrorRef]{}
	svc := newPgMirroredService(f, m)
	ctx := f.ctx(t)
	at := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)

	tx, err := svc.Record(ctx, transaction.RecordInput{
		WalletID: f.walletA, Kind: transaction.KindExpense, Amount: money.New(10_000, money.IDR), CategoryID: &f.category, OccurredAt: at,
	})
	require.NoError(t, err)
	_, err = svc.Revise(ctx, tx.ID, transaction.RevisePatch{WalletID: &f.walletB})
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, tx.ID))

	require.Len(t, m.Marks(), 3)
	require.Len(t, m.Kicks(), 3)
	for i := range 3 {
		assert.True(t, m.Marks()[i].InTx, "mark %d runs inside the unit of work", i)
		assert.False(t, m.Kicks()[i].InTx, "kick %d runs after the commit", i)
	}
}

func TestPostgres_Transaction_AFailedMarkRollsBackTheRecord(t *testing.T) {
	f := newPgFixture(t)
	m := &fakes.Mirror[transaction.MirrorRef]{MarkErr: errors.New("state table gone")}
	svc := newPgMirroredService(f, m)
	ctx := f.ctx(t)

	_, err := svc.Record(ctx, transaction.RecordInput{
		WalletID: f.walletA, Kind: transaction.KindExpense, Amount: money.New(10_000, money.IDR),
		OccurredAt: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC),
	})
	require.Error(t, err)
	require.Empty(t, m.Kicks())

	items, err := f.store.List(ctx, f.tc.OrgID, f.tc.ProjectID, transaction.ListOpts{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, items, "the row write and the mark commit together or not at all")
}
