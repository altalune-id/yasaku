package report

import (
	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

type walletMovement struct {
	WalletID         uuid.UUID
	Name             string
	Kind             string
	ExcludeFromTotal bool
	Opening          int64
	In               int64
	Out              int64
}

func (m walletMovement) line(currency money.Currency) WalletLine {
	return WalletLine{
		WalletID:         m.WalletID,
		Name:             m.Name,
		Kind:             m.Kind,
		ExcludeFromTotal: m.ExcludeFromTotal,
		Opening:          money.New(m.Opening, currency),
		In:               money.New(m.In, currency),
		Out:              money.New(m.Out, currency),
		Closing:          money.New(m.Opening+m.In-m.Out, currency),
	}
}

func assembleSummary(ref PeriodRef, currency money.Currency, income, expense int64, txCount int, moves []walletMovement) PeriodSummary {
	out := PeriodSummary{
		Period:         ref,
		Currency:       currency,
		Income:         money.New(income, currency),
		Expense:        money.New(expense, currency),
		Net:            money.New(income-expense, currency),
		TxCount:        txCount,
		SpendableTotal: money.Zero(currency),
		Total:          money.Zero(currency),
	}
	if len(moves) == 0 {
		return out
	}
	out.Wallets = make([]WalletLine, 0, len(moves))
	for _, m := range moves {
		out.Wallets = append(out.Wallets, m.line(currency))
	}
	totals := TotalsOf(out.Wallets, currency)
	out.SpendableTotal, out.Total = totals.Spendable, totals.Total
	return out
}

// TotalsOf is the one headline rule: it sums the lines held in currency, the project's ledger currency; Spendable leaves out the wallets excluded from the total, Total keeps them, and nothing is converted from another currency.
func TotalsOf(lines []WalletLine, currency money.Currency) WalletTotals {
	out := WalletTotals{Spendable: money.Zero(currency), Total: money.Zero(currency)}
	for _, l := range lines {
		if l.Closing.Currency != currency {
			out.Mixed = true
			continue
		}
		out.Total = out.Total.Add(l.Closing)
		if !l.ExcludeFromTotal {
			out.Spendable = out.Spendable.Add(l.Closing)
		}
	}
	return out
}

func assignShares(slices []CategorySlice) []CategorySlice {
	var total int64
	for _, s := range slices {
		total += s.Amount.Minor
	}
	if total == 0 {
		return slices
	}
	for i := range slices {
		slices[i].Share = float64(slices[i].Amount.Minor) / float64(total)
	}
	return slices
}

func orderCashflow(periodIDs []uuid.UUID, found map[uuid.UUID]CashflowPoint) []CashflowPoint {
	out := make([]CashflowPoint, 0, len(periodIDs))
	for _, id := range periodIDs {
		p, ok := found[id]
		if !ok {
			continue
		}
		out = append(out, p)
	}
	return out
}
