// Package report is the read-only reporting bounded context over the ledger's transactions.
package report

import (
	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/money"
)

// PeriodRef identifies a budget period in a report without pulling in the period aggregate.
type PeriodRef struct {
	ID    uuid.UUID
	Name  string
	Start civil.Date
	End   *civil.Date
}

// WalletLine is one wallet's movement across a period, or its live balance when the report is not period-keyed.
type WalletLine struct {
	WalletID         uuid.UUID
	Name             string
	Kind             string
	ExcludeFromTotal bool
	Opening          money.Amount
	In               money.Amount
	Out              money.Amount
	Closing          money.Amount
}

// PeriodSummary is one period's headline totals in a single currency.
type PeriodSummary struct {
	Period         PeriodRef
	Currency       money.Currency
	Income         money.Amount
	Expense        money.Amount
	Net            money.Amount
	TxCount        int
	Wallets        []WalletLine
	SpendableTotal money.Amount
	Total          money.Amount
}

// CategorySlice is one category's share of a period's spend or income; a nil CategoryID is the uncategorized slice.
type CategorySlice struct {
	CategoryID *uuid.UUID
	Name       string
	Icon       string
	Color      string
	Amount     money.Amount
	Share      float64
	Count      int
}

// CashflowPoint is one period's income, expense and net, for a trend across periods.
type CashflowPoint struct {
	Period  PeriodRef
	Income  money.Amount
	Expense money.Amount
	Net     money.Amount
}

// Flow is the expense flowing from one wallet into one category within a period.
type Flow struct {
	WalletID     uuid.UUID
	WalletName   string
	CategoryID   *uuid.UUID
	CategoryName string
	Amount       money.Amount
}
