package handlers

import (
	"context"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/web/templates"
)

func periodLabel(p, prev, next *period.Period) templates.PeriodLabel {
	return templates.PeriodLabel{
		Name:    p.Name,
		Start:   p.StartDate,
		End:     p.EndDate,
		Monthly: p.MonthNamed(),
		Shared:  period.SharesMonth(p, prev, next),
	}
}

func labelsOf(newestFirst []*period.Period) map[uuid.UUID]templates.PeriodLabel {
	at := func(i int) *period.Period {
		if i < 0 || i >= len(newestFirst) {
			return nil
		}
		return newestFirst[i]
	}
	out := make(map[uuid.UUID]templates.PeriodLabel, len(newestFirst))
	for i, p := range newestFirst {
		out[p.ID] = periodLabel(p, at(i+1), at(i-1))
	}
	return out
}

func periodLabels(ctx context.Context, periods *period.Service) (map[uuid.UUID]templates.PeriodLabel, error) {
	items, err := periods.List(ctx, period.ListOpts{})
	if err != nil {
		return nil, err
	}
	return labelsOf(items), nil
}

func firstOpenDate(open, closed *period.Period) (civil.Date, bool) {
	if open != nil {
		return open.StartDate, true
	}
	if closed != nil && closed.EndDate != nil {
		return closed.EndDate.AddDays(1), true
	}
	return civil.Date{}, false
}
