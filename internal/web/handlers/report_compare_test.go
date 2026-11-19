package handlers_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

type reportCompareJSON struct {
	Categories []string `json:"categories"`
	Current    []int64  `json:"current"`
	Previous   []int64  `json:"previous"`
}

type reportTrendJSON struct {
	Labels  []string `json:"labels"`
	Values  []int64  `json:"values"`
	Current int      `json:"current"`
}

var reportDeltaAttr = regexp.MustCompile(`data-delta="([^"]*)"`)

func reportDeltas(body string) []string {
	var out []string
	for _, m := range reportDeltaAttr.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func (f *reportFixture) seedTwoPeriods(t *testing.T, sc reportScope) (prev, cur report.PeriodRef) {
	t.Helper()
	p := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.August, Day: 1}, "", true)
	c := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "", false)
	return report.PeriodRef{ID: p.ID, Start: p.StartDate}, report.PeriodRef{ID: c.ID, Start: c.StartDate}
}

func TestReportHandler_GetReports_ComparesWithThePreviousPeriodsOwnReport(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	prev, cur := f.seedTwoPeriods(t, sc)
	food, rent := reportCatID(), reportCatID()
	f.Reader.SummaryBy = map[uuid.UUID]report.PeriodSummary{
		cur.ID:  {Period: cur, Currency: money.IDR, Income: reportIDR(1000), Expense: reportIDR(880), Net: reportIDR(120), Adjustments: reportIDR(-50)},
		prev.ID: {Period: prev, Currency: money.IDR, Income: reportIDR(1000), Expense: reportIDR(1000), Net: reportIDR(0)},
	}
	f.Reader.SpendBy = map[uuid.UUID][]report.CategorySlice{
		cur.ID:  {{CategoryID: food, Name: "Food", Amount: reportIDR(500), Share: 0.57}, {CategoryID: rent, Name: "Rent", Amount: reportIDR(380), Share: 0.43}},
		prev.ID: {{CategoryID: food, Name: "Food", Amount: reportIDR(600), Share: 0.6}, {CategoryID: rent, Name: "Rent", Amount: reportIDR(400), Share: 0.4}},
	}

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.ElementsMatch(t, []string{"0%", "-12%"}, reportDeltas(body), "money in and out compare; net has no delta while the previous net is zero")
	var cmp reportCompareJSON
	require.NoError(t, json.Unmarshal(reportPayloadFor(t, body, templates.ReportCompareID), &cmp))
	assert.Equal(t, []string{"Food", "Rent"}, cmp.Categories)
	assert.Equal(t, []int64{500, 380}, cmp.Current)
	assert.Equal(t, []int64{600, 400}, cmp.Previous)
	assert.Contains(t, body, "data-report-compare")

	var trend reportTrendJSON
	require.NoError(t, json.Unmarshal(reportPayloadFor(t, body, "report-trend-0"), &trend))
	assert.Equal(t, []int64{600, 500}, trend.Values, "oldest first, the selected period last")
	assert.Equal(t, 1, trend.Current)
	assert.Equal(t, []string{"Aug", "Sep"}, trend.Labels)

	summaries := 0
	for _, c := range f.Reader.Calls() {
		if c.Method == "Summary" && c.PeriodID == prev.ID {
			summaries++
		}
	}
	assert.Equal(t, 1, summaries, "the comparison runs the existing period report for the previous period")
}

func TestReportHandler_GetReports_FirstPeriodHasNoComparison(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "", false)
	f.seedData(report.PeriodRef{ID: cur.ID, Start: cur.StartDate})

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Empty(t, reportDeltas(body))
	assert.NotContains(t, body, `data-chart-for="`+templates.ReportCompareID+`"`)
	assert.NotContains(t, body, "data-report-compare")
	assert.NotContains(t, body, "data-trend-compare")
}

func TestReportHandler_GetReports_OneCategoryKeepsTheSentenceButDrawsNoComparisonChart(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	prev, cur := f.seedTwoPeriods(t, sc)
	food := reportCatID()
	f.Reader.Summaries = report.PeriodSummary{Currency: money.IDR, Income: reportIDR(0), Expense: reportIDR(500), Net: reportIDR(-500)}
	f.Reader.SpendBy = map[uuid.UUID][]report.CategorySlice{
		cur.ID:  {{CategoryID: food, Name: "Food", Amount: reportIDR(500), Share: 1}},
		prev.ID: {{CategoryID: food, Name: "Food", Amount: reportIDR(400), Share: 0.8}, {CategoryID: reportCatID(), Name: "Rent", Amount: reportIDR(100), Share: 0.2}},
	}

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.NotContains(t, body, `data-chart-for="`+templates.ReportCompareID+`"`, "one category has nothing to compare side by side")
	assert.Contains(t, body, "data-report-compare", "the comparison sentence stays")
	var donut reportDonutJSON
	require.NoError(t, json.Unmarshal(reportPayloadFor(t, body, templates.ReportDonutID), &donut))
	assert.Len(t, donut.Items, 1)
}

func TestReportHandler_GetReports_BalanceFixesAreTheirOwnLine(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "", false)
	ref := report.PeriodRef{ID: cur.ID, Start: cur.StartDate}
	f.seedData(ref)
	f.Reader.Summaries.Adjustments = reportIDR(-5000000)
	f.Reader.Summaries.Openings = reportIDR(100000000)

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	assert.Contains(t, body, "data-balance-fixes")
	assert.Contains(t, body, "data-starting-balances")
	assert.Contains(t, body, reportMoney(900000000), "money in stays the summary's income, without balance fixes or starting balances")
}

func TestReportHandler_GetReports_ACurrencyChangeDropsTheComparison(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	prev, cur := f.seedTwoPeriods(t, sc)
	f.seedData(cur)
	usd := f.Reader.Summaries
	usd.Currency = money.Currency("USD")
	f.Reader.SummaryBy = map[uuid.UUID]report.PeriodSummary{cur.ID: f.Reader.Summaries, prev.ID: usd}

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, reportDeltas(rec.Body.String()))
	assert.False(t, strings.Contains(rec.Body.String(), "data-report-compare"))
}

func TestReportHandler_GetReports_NetHasNoDeltaWhenThisPeriodNetsZero(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	prev, cur := f.seedTwoPeriods(t, sc)
	f.Reader.SummaryBy = map[uuid.UUID]report.PeriodSummary{
		cur.ID:  {Period: cur, Currency: money.IDR, Income: reportIDR(500), Expense: reportIDR(500), Net: reportIDR(0)},
		prev.ID: {Period: prev, Currency: money.IDR, Income: reportIDR(400), Expense: reportIDR(300), Net: reportIDR(100)},
	}

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.ElementsMatch(t, []string{"+25%", "+67%"}, reportDeltas(rec.Body.String()), "a zero net on either side shows no net delta")
}

func TestReportHandler_GetReports_DonutAndSankeyReadOneColorPerSlot(t *testing.T) {
	t.Parallel()
	f := newReportFixture(t)
	sc := f.seedScope(t)
	cur := f.seedPeriod(t, sc, civil.Date{Year: 2026, Month: time.September, Day: 1}, "", false)
	f.seedData(report.PeriodRef{ID: cur.ID, Start: cur.StartDate})

	rec := f.get(t, sc, "")
	require.Equal(t, http.StatusOK, rec.Code)
	donut, sankey := reportDonutAndSankey(t, rec.Body.String())
	nodeColor := map[string]string{}
	for _, n := range sankey.Nodes {
		nodeColor[n.Name] = n.Color
	}
	require.Len(t, donut.Items, 6)
	for i, it := range donut.Items {
		slot := i + 1
		if it.Name == reportOtherLabel {
			slot = 0
		}
		want := templates.ReportSlotColor(slot).Var
		assert.Equal(t, want, it.Color, it.Name)
		if c, ok := nodeColor[it.Name]; ok {
			assert.Equal(t, want, c, "the Sankey node of %s shares its legend color", it.Name)
		}
	}
}
