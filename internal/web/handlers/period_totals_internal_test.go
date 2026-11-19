package handlers

import (
	"testing"

	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/money"
)

func TestPeriodTotalsOf_EveryLineCarriesACurrency(t *testing.T) {
	t.Parallel()
	open := &period.Period{Status: period.StatusOpen}
	withEntries := report.PeriodSummary{Currency: money.IDR, Income: money.New(9, money.IDR), Expense: money.New(2, money.IDR),
		Net: money.New(7, money.IDR), Adjustments: money.New(3, money.IDR), Openings: money.New(4, money.IDR)}
	closedInUSD := &period.Period{Status: period.StatusClosed, Snapshot: &period.Snapshot{Currency: money.Currency("USD"), Adjustments: 5, Openings: 7}}
	cases := []struct {
		name string
		p    *period.Period
		s    report.PeriodSummary
		want money.Currency
	}{
		{"open period with no entries", open, report.PeriodSummary{}, money.IDR},
		{"open period with entries", open, withEntries, money.IDR},
		{"closed before the currency changed", closedInUSD, report.PeriodSummary{Currency: money.IDR}, money.Currency("USD")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := periodTotalsOf(tc.p, tc.s, money.IDR)
			for line, a := range map[string]money.Amount{"income": got.Income, "expense": got.Expense, "net": got.Net, "balance fixes": got.Adjustments, "starting balances": got.Openings} {
				if a.Currency != tc.want {
					t.Errorf("%s currency = %q, want %q", line, a.Currency, tc.want)
				}
			}
		})
	}
}
