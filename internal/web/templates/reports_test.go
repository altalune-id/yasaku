package templates

import (
	"regexp"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/money"
)

func idr(n int64) money.Amount { return money.New(n, money.IDR) }

func reportsFixture() ReportsView {
	food, prev := idr(300), idr(200)
	return ReportsView{
		ProjectSlug: "home",
		PeriodID:    "cur",
		Period:      PeriodLabel{Name: "Nov 2026"},
		Income:      idr(1000), Expense: idr(400), Net: idr(600),
		Spend: []ReportSpendRow{
			{Name: "Food", Amount: food, Share: 75, Slot: 1, Trend: `{"labels":[]}`, PrevAmount: &prev},
			{Name: "report.other", Amount: idr(100), Share: 25, Slot: 0},
		},
		Donut: `{"items":[]}`,
	}
}

func reportsPage(t *testing.T, v ReportsView) string {
	t.Helper()
	return render(t, ReportsLayout(web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}, v))
}

func TestReports_FirstPeriodHasNoComparison(t *testing.T) {
	got := reportsPage(t, reportsFixture())
	for _, banned := range []string{"rep-cmp-h", "report.delta", "data-trend-compare"} {
		if strings.Contains(got, banned) {
			t.Errorf("first period shows %s", banned)
		}
	}
}

func TestReports_PreviousPeriodComparesEvenWithOneCategory(t *testing.T) {
	v := reportsFixture()
	v.Previous, v.PrevExpense = &PeriodLabel{Name: "Oct"}, idr(500)
	d := -20
	v.ExpenseDelta = &d
	got := reportsPage(t, v)
	requireContains(t, got, "rep-cmp-h", "report.compare_less", "data-trend-compare", `data-delta="-20%"`)
	if strings.Contains(got, ReportCompareID) {
		t.Error("compare chart drawn without a compare payload")
	}

	v.PrevExpense = idr(0)
	requireContains(t, reportsPage(t, v), "report.compare_prev_zero")
}

func TestReports_LegendOpensTrendsExceptOther(t *testing.T) {
	got := reportsPage(t, reportsFixture())
	requireContains(t, got, `data-dialog-open="rep-trend-0"`, `id="rep-trend-0"`, "bg-primary", "progress-primary", "bg-secondary")
	if strings.Contains(got, "rep-trend-1") {
		t.Error("Other opens a trend")
	}
}

func TestReports_ChartsLoadOnlyWhenDrawnAndJSOnlyCardsStartHidden(t *testing.T) {
	v := reportsFixture()
	v.Sankey, v.Bars = `{"nodes":[]}`, `{"categories":[]}`
	got := reportsPage(t, v)
	requireContains(t, got, "echarts.min.js", `data-needs-js hidden`)

	empty := reportsFixture()
	empty.Spend, empty.Donut = nil, ""
	got = reportsPage(t, empty)
	requireContains(t, got, "report.empty_title", `data-dialog-open="tx-add-dialog"`)
	if strings.Contains(got, "echarts.min.js") || strings.Contains(got, "charts.js") {
		t.Error("ECharts loads on a report without charts")
	}
}

func TestReports_ExportDownloadsTheZip(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	requireContains(t, render(t, ReportsPage(d, reportsFixture())), `href="/orgs/acme/projects/home/export.zip"`, "report.export")
}

func TestPercentChange_RoundsHalfAwayFromZero(t *testing.T) {
	for _, tc := range []struct {
		cur, prev int64
		want      int
		ok        bool
	}{{88, 100, -12, true}, {108, 100, 8, true}, {1, 8, -88, true}, {3, 2, 50, true}, {5, 200, -98, true}, {1, 3, -67, true}, {100, 100, 0, true}, {-50, -100, 50, true}, {-150, -100, -50, true}, {5, 0, 0, false}} {
		got, ok := PercentChange(idr(tc.cur), idr(tc.prev))
		if got != tc.want || ok != tc.ok {
			t.Errorf("PercentChange(%d, %d) = %d, %v; want %d, %v", tc.cur, tc.prev, got, ok, tc.want, tc.ok)
		}
	}
}

func TestReports_ScreenReaderTablesSitInsideAnSROnlyBox(t *testing.T) {
	v := reportsFixture()
	v.Sankey, v.Bars = `{"nodes":[]}`, `{"categories":[]}`
	v.Flows = []ReportFlowRow{{Wallet: "Cash", Category: "Food", Amount: idr(1)}}
	v.Cashflow = []ReportCashflowRow{{Label: "Nov"}}
	v.Spend[0].TrendPoints = []ReportTrendPoint{{Label: "Nov", Amount: idr(1)}}
	got := reportsPage(t, v)
	if regexp.MustCompile(`<table[^>]*\bsr-only\b`).MatchString(got) {
		t.Error("a <table> carries sr-only; tables ignore the 1px box, wrap them in a div.sr-only")
	}
	if n := strings.Count(got, `<div class="sr-only"><table`); n != 3 {
		t.Errorf("sr-only wrapped tables = %d, want 3 (flows, cashflow, trend)", n)
	}
}

func TestReports_WalletMovementStacksOnPhones(t *testing.T) {
	v := reportsFixture()
	v.Wallets = []ReportWalletRow{{Name: "Cash", Opening: "Rp 1", In: "Rp 2", Out: "Rp 3", Closing: "Rp 4", Excluded: true}}
	v.Totals = []ReportWalletTotal{{Kind: "all", Label: "report.total_all", Closing: "Rp 4"}}
	got := reportsPage(t, v)
	requireContains(t, got, `data-report-wallet-cards class="flex flex-col divide-y divide-base-300 sm:hidden"`, `class="hidden overflow-x-auto sm:block"`)
	cards := got[strings.Index(got, "data-report-wallet-cards"):]
	cards = cards[:strings.Index(cards, "</ul>")]
	requireContains(t, cards, "Cash", "wallet.exclude_from_total", "report.opening", "Rp 1", "report.wallet_in", "Rp 2", "report.wallet_out", "Rp 3", "report.closing", "Rp 4")
	requireContains(t, got, `data-report-total-card="all"`)
}
