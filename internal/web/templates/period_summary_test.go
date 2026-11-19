package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/money"
)

func TestPeriodNavBar_LinksToThePagePath(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	got := render(t, PeriodNavBar(d, PeriodNav{ProjectSlug: "home", Path: "/reports", PrevPeriodID: "a", Period: PeriodLabel{Name: "Payday"}}))
	requireContains(t, got, `href="/orgs/acme/projects/home/reports?period=a"`, `>Payday</label>`, `disabled aria-label="overview.next_period"`)
}

func TestPeriodTotals_DeltasShowOnlyWithAPreviousPeriod(t *testing.T) {
	d := web.LayoutData{}
	idr := func(n int64) money.Amount { return money.New(n, money.IDR) }
	less, more := -12, 8
	base := PeriodTotals{Income: idr(100), Expense: idr(50), Net: idr(50)}

	if got := render(t, periodTotalsBody(d, base)); strings.Contains(got, "report.delta") {
		t.Error("first period shows a delta")
	}
	withPrev := base
	withPrev.Previous = &PeriodLabel{Name: "Oct"}
	withPrev.IncomeDelta, withPrev.ExpenseDelta = &more, &less
	got := render(t, periodTotalsBody(d, withPrev))
	if n := strings.Count(got, "report.delta"); n != 2 {
		t.Errorf("deltas = %d, want 2 (net has none)", n)
	}
	requireContains(t, got, `data-delta="+8%"`, `data-delta="-12%"`)
}
