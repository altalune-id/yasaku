package opensheetsync_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/money"
)

func TestTransactionRow_Golden(t *testing.T) {
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	got := opensheetsync.TransactionRow(opensheetsync.TransactionFacts{
		ID:           uuid.MustParse("0198c2a0-0000-7000-8000-000000000001"),
		Kind:         "expense",
		Amount:       money.New(4_000_000, money.IDR),
		OccurredAt:   time.Date(2026, 8, 31, 20, 30, 0, 0, time.UTC),
		Location:     jakarta,
		WalletID:     uuid.MustParse("0198c2a0-0000-7000-8000-0000000000a1"),
		WalletName:   "BCA",
		CategoryID:   uuid.MustParse("0198c2a0-0000-7000-8000-0000000000c1"),
		CategoryName: "Food",
		PeriodName:   "Sep 2026",
		Note:         "nasi goreng",
		UpdatedAt:    time.Date(2026, 9, 1, 1, 2, 3, 0, jakarta),
	})
	require.Equal(t, map[string]string{
		"id": "0198c2a0-0000-7000-8000-000000000001", "date": "2026-09-01", "occurred_at": "2026-09-01T03:30:00+07:00",
		"kind": "expense", "amount": "40000", "currency": "IDR",
		"wallet": "BCA", "wallet_id": "0198c2a0-0000-7000-8000-0000000000a1", "to_wallet": "", "to_wallet_id": "",
		"category": "Food", "category_id": "0198c2a0-0000-7000-8000-0000000000c1",
		"note": "nasi goreng", "period": "Sep 2026", "recurring_id": "", "updated_at": "2026-08-31T18:02:03Z",
	}, got)
}

func TestTransactionRow_TransferAndCents(t *testing.T) {
	to := uuid.MustParse("0198c2a0-0000-7000-8000-0000000000a2")
	got := opensheetsync.TransactionRow(opensheetsync.TransactionFacts{
		Kind: "transfer", Amount: money.New(1250, money.Currency("USD")), OccurredAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		ToWalletID: to, ToWalletName: "Savings",
	})
	require.Equal(t, "12.50", got["amount"])
	require.Equal(t, "USD", got["currency"])
	require.Equal(t, to.String(), got["to_wallet_id"])
	require.Equal(t, "Savings", got["to_wallet"])
	require.Equal(t, "2026-01-02", got["date"], "a nil Location means UTC")
	require.Equal(t, "", got["category_id"], "uuid.Nil renders empty")
}

func TestWalletRow_Golden(t *testing.T) {
	got := opensheetsync.WalletRow(opensheetsync.WalletFacts{
		ID: uuid.MustParse("0198c2a0-0000-7000-8000-0000000000a1"), Name: "BCA", Kind: "bank", Provider: "BCA",
		Balance: money.New(-150_000, money.IDR), ExcludeFromTotal: true, Archived: false,
		UpdatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	require.Equal(t, map[string]string{
		"id": "0198c2a0-0000-7000-8000-0000000000a1", "name": "BCA", "kind": "bank", "provider": "BCA",
		"currency": "IDR", "balance": "-1500", "exclude_from_total": "true", "archived": "false",
		"updated_at": "2026-09-01T00:00:00Z",
	}, got)
}

func TestCategoryRow_Golden(t *testing.T) {
	got := opensheetsync.CategoryRow(opensheetsync.CategoryFacts{
		ID: uuid.MustParse("0198c2a0-0000-7000-8000-0000000000c1"), Name: "Food", Kind: "expense",
		Icon: "utensils", Color: "chart-1", Archived: true, UpdatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	require.Equal(t, map[string]string{
		"id": "0198c2a0-0000-7000-8000-0000000000c1", "name": "Food", "kind": "expense", "icon": "utensils",
		"color": "chart-1", "archived": "true", "updated_at": "2026-09-01T00:00:00Z",
	}, got)
}
