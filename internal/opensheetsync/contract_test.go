package opensheetsync_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
)

// NOTE: this is the users' sheet contract. Editing an expected row here is a breaking change for every linked sheet.
func TestContract_IsPinned(t *testing.T) {
	want := map[opensheetsync.Entity][]string{
		opensheetsync.EntityTransaction: {"id", "date", "occurred_at", "kind", "amount", "currency", "wallet", "wallet_id",
			"to_wallet", "to_wallet_id", "category", "category_id", "note", "period", "recurring_id", "updated_at", "deleted_at"},
		opensheetsync.EntityWallet:   {"id", "name", "kind", "provider", "currency", "balance", "exclude_from_total", "archived", "updated_at", "deleted_at"},
		opensheetsync.EntityCategory: {"id", "name", "kind", "icon", "color", "archived", "updated_at", "deleted_at"},
	}
	tabs := opensheetsync.Contract()
	require.Len(t, tabs, 3)
	require.Equal(t, []opensheetsync.Entity{opensheetsync.EntityTransaction, opensheetsync.EntityWallet, opensheetsync.EntityCategory},
		[]opensheetsync.Entity{tabs[0].Entity, tabs[1].Entity, tabs[2].Entity}, "tutorial order")
	for _, tab := range tabs {
		require.Equal(t, want[tab.Entity], tab.Columns, tab.Entity)
		require.Equal(t, "id", tab.Columns[0])
		require.Equal(t, "deleted_at", tab.Columns[len(tab.Columns)-1])
		require.NotContains(t, tab.Written(), "deleted_at", "opensheet writes deleted_at, yasaku never does")
	}
	require.Equal(t, opensheetsync.SheetSlugs{Transactions: "yasaku-transactions", Wallets: "yasaku-wallets", Categories: "yasaku-categories"},
		opensheetsync.DefaultSheetSlugs())
}

func TestTab_HeaderTSVPastesIntoOneRow(t *testing.T) {
	tab, ok := opensheetsync.TabFor(opensheetsync.EntityCategory)
	require.True(t, ok)
	require.Equal(t, "id\tname\tkind\ticon\tcolor\tarchived\tupdated_at\tdeleted_at", tab.HeaderTSV())
}

func TestMappers_WriteExactlyTheContractColumnsYasakuOwns(t *testing.T) {
	rows := map[opensheetsync.Entity]map[string]string{
		opensheetsync.EntityTransaction: opensheetsync.TransactionRow(opensheetsync.TransactionFacts{}),
		opensheetsync.EntityWallet:      opensheetsync.WalletRow(opensheetsync.WalletFacts{}),
		opensheetsync.EntityCategory:    opensheetsync.CategoryRow(opensheetsync.CategoryFacts{}),
	}
	for _, tab := range opensheetsync.Contract() {
		got := slices.Sorted(maps.Keys(rows[tab.Entity]))
		require.Equal(t, slices.Sorted(slices.Values(tab.Written())), got, tab.Entity)
	}
}
