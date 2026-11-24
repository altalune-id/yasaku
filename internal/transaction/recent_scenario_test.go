package transaction_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

type recentScenario struct {
	tc, sibling                  tenant.Context
	walletA, walletB, sibWallet  uuid.UUID
	food, coffee, salary, sibCat uuid.UUID
	since                        time.Time
}

func (sc recentScenario) save(t *testing.T, store transaction.Store, tc tenant.Context, p transaction.NewParams) {
	t.Helper()
	p.CreatedBy = tc.UserID
	tx, err := transaction.New(tc.OrgID, tc.ProjectID, p)
	require.NoError(t, err)
	require.NoError(t, store.Save(fakes.Owner(t.Context(), tc), tx))
}

func (sc recentScenario) expense(t *testing.T, store transaction.Store, cat uuid.UUID, wallet uuid.UUID, note string, minor int64, at time.Time) {
	t.Helper()
	sc.save(t, store, sc.tc, transaction.NewParams{
		WalletID: wallet, Kind: transaction.KindExpense, Amount: money.New(minor, money.IDR),
		CategoryID: &cat, Note: note, OccurredAt: at,
	})
}

func (sc recentScenario) checkTopCategories(t *testing.T, store transaction.Store) {
	t.Helper()
	before := sc.since.Add(-time.Second)
	in := sc.since.Add(24 * time.Hour)
	sc.expense(t, store, sc.food, sc.walletA, "a", 100, sc.since)
	sc.expense(t, store, sc.food, sc.walletA, "b", 100, in)
	sc.expense(t, store, sc.food, sc.walletA, "c", 100, in)
	sc.expense(t, store, sc.coffee, sc.walletA, "d", 100, in)
	sc.expense(t, store, sc.coffee, sc.walletA, "e", 100, in)
	for range 3 {
		sc.expense(t, store, sc.coffee, sc.walletA, "old", 100, before)
	}
	sc.save(t, store, sc.tc, transaction.NewParams{
		WalletID: sc.walletA, Kind: transaction.KindIncome, Amount: money.New(100, money.IDR),
		CategoryID: &sc.salary, OccurredAt: in,
	})
	sc.save(t, store, sc.tc, transaction.NewParams{
		WalletID: sc.walletA, ToWalletID: &sc.walletB, Kind: transaction.KindTransfer,
		Amount: money.New(100, money.IDR), OccurredAt: in,
	})
	for range 5 {
		sc.save(t, store, sc.sibling, transaction.NewParams{
			WalletID: sc.sibWallet, Kind: transaction.KindExpense, Amount: money.New(100, money.IDR),
			CategoryID: &sc.sibCat, OccurredAt: in,
		})
	}

	ctx := fakes.Owner(t.Context(), sc.tc)
	got, err := store.TopCategories(ctx, sc.tc.OrgID, sc.tc.ProjectID, sc.since, 10)
	require.NoError(t, err)
	require.Equal(t, []transaction.CategoryCount{
		{CategoryID: sc.food, Count: 3},
		{CategoryID: sc.coffee, Count: 2},
		{CategoryID: sc.salary, Count: 1},
	}, got)

	got, err = store.TopCategories(ctx, sc.tc.OrgID, sc.tc.ProjectID, sc.since, 2)
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func (sc recentScenario) checkRecentByCategory(t *testing.T, store transaction.Store) {
	t.Helper()
	day := func(d int) time.Time { return sc.since.AddDate(0, 0, d) }
	sc.expense(t, store, sc.food, sc.walletA, "Soto", 20000, day(0))
	sc.expense(t, store, sc.food, sc.walletA, "Nasi goreng", 30000, day(1))
	sc.expense(t, store, sc.food, sc.walletA, "Nasi goreng", 25000, day(2))
	sc.expense(t, store, sc.food, sc.walletB, "nasi  GORENG", 25000, day(3))
	sc.expense(t, store, sc.food, sc.walletA, "Bakso", 15000, day(4))
	sc.expense(t, store, sc.coffee, sc.walletA, "Kopi", 18000, day(5))
	sc.save(t, store, sc.sibling, transaction.NewParams{
		WalletID: sc.sibWallet, Kind: transaction.KindExpense, Amount: money.New(99000, money.IDR),
		CategoryID: &sc.sibCat, Note: "sibling", OccurredAt: day(6),
	})

	ctx := fakes.Owner(t.Context(), sc.tc)
	got, err := store.RecentByCategory(ctx, sc.tc.OrgID, sc.tc.ProjectID, sc.food, 3)
	require.NoError(t, err)
	require.Equal(t, []transaction.RecentEntry{
		{Note: "Bakso", Amount: money.New(15000, money.IDR), WalletID: sc.walletA},
		{Note: "nasi  GORENG", Amount: money.New(25000, money.IDR), WalletID: sc.walletB},
		{Note: "Nasi goreng", Amount: money.New(30000, money.IDR), WalletID: sc.walletA},
	}, got)

	got, err = store.RecentByCategory(ctx, sc.tc.OrgID, sc.tc.ProjectID, sc.sibCat, 3)
	require.NoError(t, err)
	require.Empty(t, got, "a sibling project's category must not leak")
}
