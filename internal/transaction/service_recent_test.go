package transaction_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/transaction"
)

func TestService_TopCategories(t *testing.T) {
	f := newFixture(t)
	f.expense(t, augustDay(1), 20_000, "a")
	f.expense(t, augustDay(9), 20_000, "b")
	f.expense(t, augustDay(10), 20_000, "c")

	got, err := f.svc.TopCategories(f.ctx, augustDay(9), 5)
	require.NoError(t, err)
	require.Equal(t, []transaction.CategoryCount{{CategoryID: f.expenseCat, Count: 2}}, got)

	_, err = f.svc.TopCategories(context.Background(), augustDay(9), 5)
	require.Error(t, err, "no tenant scope")
}

func TestService_RecentByCategory(t *testing.T) {
	f := newFixture(t)
	f.expense(t, augustDay(1), 20_000, "Kopi")
	f.expense(t, augustDay(2), 20_000, "kopi")
	f.expense(t, augustDay(3), 25_000, "Jus")

	got, err := f.svc.RecentByCategory(f.ctx, f.expenseCat, 5)
	require.NoError(t, err)
	require.Equal(t, []transaction.RecentEntry{
		{Note: "Jus", Amount: idr(25_000), WalletID: f.walletIDR},
		{Note: "kopi", Amount: idr(20_000), WalletID: f.walletIDR},
	}, got)

	_, err = f.svc.RecentByCategory(context.Background(), f.expenseCat, 5)
	require.Error(t, err, "no tenant scope")
}

func TestService_RecentQueriesClampANonPositiveLimit(t *testing.T) {
	f := newFixture(t)
	f.expense(t, augustDay(1), 20_000, "Kopi")
	for _, limit := range []int{0, -1} {
		top, err := f.svc.TopCategories(f.ctx, augustDay(1), limit)
		require.NoError(t, err)
		require.Len(t, top, 1, "limit %d", limit)
		recent, err := f.svc.RecentByCategory(f.ctx, f.expenseCat, limit)
		require.NoError(t, err)
		require.Len(t, recent, 1, "limit %d", limit)
	}
}
