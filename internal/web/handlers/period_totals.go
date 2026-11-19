package handlers

import (
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

func periodTotalsOf(p *period.Period, s report.PeriodSummary, cur money.Currency) templates.PeriodTotals {
	t := templates.PeriodTotals{Income: money.Zero(cur), Expense: money.Zero(cur), Net: money.Zero(cur)}
	if s.Currency != "" {
		t.Income, t.Expense, t.Net, t.Adjustments, t.Openings = s.Income, s.Expense, s.Net, s.Adjustments, s.Openings
	}
	if snap := p.Snapshot; p.Status == period.StatusClosed && snap != nil {
		t.Income, t.Expense, t.Net = money.New(snap.Income, snap.Currency), money.New(snap.Expense, snap.Currency), money.New(snap.Net, snap.Currency)
		t.Adjustments, t.Openings = money.New(snap.Adjustments, snap.Currency), money.New(snap.Openings, snap.Currency)
	}
	return t
}
