package handlers

import (
	"cmp"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

const reportPeriodWindow = 6

const reportMaxSlices = 5

//i18n:use report.other
const reportOtherKey = "report.other"

// ReportHandler renders the read-only reporting page for one project.
type ReportHandler struct {
	Deps
	Reports *report.Service
	Periods *period.Service
}

// NewReportHandler wires the handler.
func NewReportHandler(d Deps, projects *project.Service, reports *report.Service, periods *period.Service) *ReportHandler {
	d.Projects = projects
	return &ReportHandler{Deps: d, Reports: reports, Periods: periods}
}

// Register wires the report routes onto mux.
func (h *ReportHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/reports", h.GetReports)
}

// GetReports renders the totals, spending, comparison, flows and cashflow for one period.
func (h *ReportHandler) GetReports(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, "reports")
	d.Title = pageTitle(sc.req, "nav.reports", sc.project.Name)
	v := templates.ReportsView{ProjectSlug: sc.project.Slug}

	recent, err := h.Periods.List(sc.req.Context(), period.ListOpts{Limit: reportPeriodWindow})
	if err != nil {
		h.reportFailed(w, sc, d, "list periods", err)
		return
	}
	selected, ok := h.selectPeriod(w, sc, d, recent)
	if !ok {
		return
	}
	if selected == nil {
		v.NoPeriod = true
		Render(w, sc.req, templates.ReportsLayout(d, v))
		return
	}
	if err := h.fill(sc, d, recent, selected, &v); err != nil {
		h.reportFailed(w, sc, d, "fill", err)
		return
	}
	Render(w, sc.req, templates.ReportsLayout(d, v))
}

// selectPeriod resolves ?period=, falling back to the current period and then to the newest in scope. SECURITY: ByID is scope-checked, so a foreign period id renders as a 404 rather than another project's totals.
func (h *ReportHandler) selectPeriod(w http.ResponseWriter, sc ProjectScope, d web.LayoutData, recent []*period.Period) (*period.Period, bool) {
	if raw := strings.TrimSpace(sc.req.URL.Query().Get("period")); raw != "" {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_period"),
				"That link carries a malformed period id.", parseErr)
			return nil, false
		}
		p, err := h.Periods.ByID(sc.req.Context(), id)
		if err == nil {
			return p, true
		}
		if period.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, pageTitle(sc.req, "error_title.period_not_found"), "No period with that id in this project.", err)
			return nil, false
		}
		h.reportFailed(w, sc, d, "period byID", err)
		return nil, false
	}
	cur, err := h.Periods.Current(sc.req.Context())
	if err == nil {
		return cur, true
	}
	if !period.IsNotFoundError(err) {
		h.reportFailed(w, sc, d, "current period", err)
		return nil, false
	}
	if len(recent) == 0 {
		return nil, true
	}
	return recent[0], true
}

func (h *ReportHandler) cashflowWindow(sc ProjectScope, recent []*period.Period, selected *period.Period) ([]*period.Period, error) {
	if len(recent) > 0 && recent[0].ID == selected.ID {
		return recent, nil
	}
	before := selected.StartDate.AddDays(1)
	return h.Periods.List(sc.req.Context(), period.ListOpts{Limit: reportPeriodWindow, Before: &before})
}

func (h *ReportHandler) fill(sc ProjectScope, d web.LayoutData, recent []*period.Period, selected *period.Period, v *templates.ReportsView) error {
	ctx := sc.req.Context()
	window, err := h.cashflowWindow(sc, recent, selected)
	if err != nil {
		return err
	}
	prev, next, err := h.Periods.Neighbors(ctx, selected.ID)
	if err != nil {
		return err
	}
	labels, err := periodLabels(ctx, h.Periods)
	if err != nil {
		return err
	}
	summary, err := h.Reports.Summary(ctx, selected.ID)
	if err != nil {
		return err
	}
	income, err := h.Reports.IncomeByCategory(ctx, selected.ID)
	if err != nil {
		return err
	}
	points, err := h.Reports.Cashflow(ctx, reportCashflowIDs(window))
	if err != nil {
		return err
	}
	flows, err := h.Reports.Flows(ctx, selected.ID)
	if err != nil {
		return err
	}
	spendBy, err := h.spendByPeriod(sc, window, selected, prev)
	if err != nil {
		return err
	}

	currency := summary.Currency
	v.PeriodID, v.Period = selected.ID.String(), labels[selected.ID]
	if prev != nil {
		v.PrevPeriodID = prev.ID.String()
	}
	if next != nil {
		v.NextPeriodID = next.ID.String()
	}
	t := periodTotalsOf(selected, summary, currency)
	v.Income, v.Expense, v.Net, v.Adjustments, v.Openings = t.Income, t.Expense, t.Net, t.Adjustments, t.Openings

	var prevSpend []report.CategorySlice
	if prev != nil {
		ok, err := h.fillPrevious(sc, prev, labels[prev.ID], currency, v)
		if err != nil {
			return err
		}
		if ok {
			prevSpend = spendBy[prev.ID]
		}
	}

	fold := reportFoldSlices(d, spendBy[selected.ID], currency)
	trend := reportTrendPeriods(window, selected.ID, labels, d)
	v.Spend = reportSpendRows(d, fold, trend, spendBy, prevSpend, v.Previous != nil, currency)
	v.TopIncome = reportCategoryRows(d, reportFoldSlices(d, income, currency).rows)
	v.Donut = reportDonutJSON(d, fold.rows)
	if v.Previous != nil {
		v.Compare = reportCompareJSON(d, fold, prevSpend, v.Period, *v.Previous, currency)
	}
	sk := reportSankey(d, flows, fold)
	v.Sankey, v.SankeyRows, v.Flows = sk.payload, sk.rows, sk.flows
	v.Bars, v.Cashflow = reportBars(d, points, labels, currency)
	v.Wallets, v.Totals = reportWalletRows(d, summary)
	return nil
}

func (h *ReportHandler) fillPrevious(sc ProjectScope, prev *period.Period, label templates.PeriodLabel, currency money.Currency, v *templates.ReportsView) (bool, error) {
	ps, err := h.Reports.Summary(sc.req.Context(), prev.ID)
	if err != nil {
		return false, err
	}
	if ps.Currency != currency {
		return false, nil
	}
	pt := periodTotalsOf(prev, ps, currency)
	v.Previous, v.PrevExpense = &label, pt.Expense
	v.IncomeDelta, v.ExpenseDelta = reportDelta(v.Income, pt.Income), reportDelta(v.Expense, pt.Expense)
	if !v.Net.IsZero() && !pt.Net.IsZero() {
		v.NetDelta = reportDelta(v.Net, pt.Net)
	}
	return true, nil
}

func reportDelta(cur, prev money.Amount) *int {
	pct, ok := templates.PercentChange(cur, prev)
	if !ok {
		return nil
	}
	return &pct
}

func (h *ReportHandler) spendByPeriod(sc ProjectScope, window []*period.Period, also ...*period.Period) (map[uuid.UUID][]report.CategorySlice, error) {
	ids := make([]uuid.UUID, 0, len(window)+len(also))
	for _, p := range window {
		ids = append(ids, p.ID)
	}
	for _, p := range also {
		if p != nil && !slices.Contains(ids, p.ID) {
			ids = append(ids, p.ID)
		}
	}
	out := make(map[uuid.UUID][]report.CategorySlice, len(ids))
	for _, id := range ids {
		s, err := h.Reports.SpendByCategory(sc.req.Context(), id)
		if err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, nil
}

func (h *ReportHandler) reportFailed(w http.ResponseWriter, sc ProjectScope, d web.LayoutData, what string, err error) {
	h.LogErr("web report: "+what, err)
	v := templates.ReportsView{ProjectSlug: sc.project.Slug, Error: d.Tr("report.error"), ErrorCode: ErrorRef(err)}
	RenderStatus(w, sc.req, http.StatusInternalServerError, templates.ReportsLayout(d, v))
}

func reportCashflowIDs(window []*period.Period) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(window))
	for i := len(window) - 1; i >= 0; i-- {
		ids = append(ids, window[i].ID)
	}
	return ids
}

func reportWalletRows(d web.LayoutData, s report.PeriodSummary) ([]templates.ReportWalletRow, []templates.ReportWalletTotal) {
	rows := make([]templates.ReportWalletRow, 0, len(s.Wallets))
	for _, l := range s.Wallets {
		rows = append(rows, templates.ReportWalletRow{
			Name:     l.Name,
			Opening:  templates.Money(d, l.Opening),
			In:       templates.Money(d, l.In),
			Out:      templates.Money(d, l.Out),
			Closing:  templates.Money(d, l.Closing),
			Excluded: l.ExcludeFromTotal,
		})
	}
	if len(rows) == 0 {
		return rows, nil
	}
	totals := []templates.ReportWalletTotal{{
		Kind:    "spendable",
		Label:   d.Tr("report.total_spendable"),
		Closing: templates.Money(d, s.SpendableTotal),
	}}
	if s.Total.Minor != s.SpendableTotal.Minor {
		totals = append(totals, templates.ReportWalletTotal{
			Kind:    "all",
			Label:   d.Tr("report.total_all"),
			Closing: templates.Money(d, s.Total),
		})
	}
	return rows, totals
}

type reportSlice struct {
	Name   string
	Key    string
	Icon   string
	Other  bool
	Amount money.Amount
	Share  float64
}

type reportFold struct {
	rows  []reportSlice
	kept  map[string]string
	other string
}

func reportFoldSlices(d web.LayoutData, cats []report.CategorySlice, currency money.Currency) reportFold {
	names := reportNames{}
	f := reportFold{
		rows: make([]reportSlice, 0, reportMaxSlices+1),
		kept: make(map[string]string, reportMaxSlices),
	}
	other, otherShare := money.Zero(currency), 0.0
	for _, s := range cats {
		if s.Amount.Minor <= 0 || s.Amount.Currency != currency {
			continue
		}
		if len(f.rows) >= reportMaxSlices {
			other = other.Add(s.Amount)
			otherShare += s.Share
			continue
		}
		label := strings.TrimSpace(s.Name)
		if s.CategoryID == nil || label == "" {
			label = d.Tr("tx.uncategorized")
		}
		label = names.unique(label)
		key := reportCategoryKey(s.CategoryID)
		f.kept[key] = label
		f.rows = append(f.rows, reportSlice{Name: label, Key: key, Icon: s.Icon, Amount: s.Amount, Share: s.Share})
	}
	f.other = names.unique(d.Tr(reportOtherKey))
	if other.Minor > 0 {
		f.rows = append(f.rows, reportSlice{Name: f.other, Other: true, Amount: other, Share: otherShare})
	}
	return f
}

func reportCategoryKey(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func reportCategoryRows(d web.LayoutData, rows []reportSlice) []templates.ReportCategoryRow {
	shares := reportSharePercents(rows)
	out := make([]templates.ReportCategoryRow, 0, len(rows))
	for i, s := range rows {
		out = append(out, templates.ReportCategoryRow{
			Name:   s.Name,
			Amount: templates.Money(d, s.Amount),
			Share:  strconv.Itoa(shares[i]) + "%",
		})
	}
	return out
}

func reportSlot(i int, s reportSlice) int {
	if s.Other {
		return 0
	}
	return i + 1
}

func reportAmountOf(cats []report.CategorySlice, key string, currency money.Currency) money.Amount {
	sum := money.Zero(currency)
	for _, s := range cats {
		if s.Amount.Currency == currency && reportCategoryKey(s.CategoryID) == key {
			sum = sum.Add(s.Amount)
		}
	}
	return sum
}

type reportTrendPeriod struct {
	id      uuid.UUID
	short   string
	label   string
	current bool
}

func reportTrendPeriods(window []*period.Period, selected uuid.UUID, labels map[uuid.UUID]templates.PeriodLabel, d web.LayoutData) []reportTrendPeriod {
	out := make([]reportTrendPeriod, 0, len(window))
	for i := len(window) - 1; i >= 0; i-- {
		p := window[i]
		l := labels[p.ID]
		out = append(out, reportTrendPeriod{id: p.ID, short: templates.PeriodShortLabelText(d, l), label: templates.PeriodLabelText(d, l), current: p.ID == selected})
	}
	return out
}

func reportSpendRows(
	d web.LayoutData, fold reportFold, trend []reportTrendPeriod, spendBy map[uuid.UUID][]report.CategorySlice,
	prevSpend []report.CategorySlice, hasPrev bool, currency money.Currency,
) []templates.ReportSpendRow {
	shares := reportSharePercents(fold.rows)
	out := make([]templates.ReportSpendRow, 0, len(fold.rows))
	for i, s := range fold.rows {
		row := templates.ReportSpendRow{Name: s.Name, Icon: s.Icon, Amount: s.Amount, Share: shares[i], Slot: reportSlot(i, s)}
		if !s.Other {
			row.Trend, row.TrendPoints = reportTrendJSON(d, s.Key, trend, spendBy, currency)
			if hasPrev {
				prev := reportAmountOf(prevSpend, s.Key, currency)
				row.PrevAmount = &prev
			}
		}
		out = append(out, row)
	}
	return out
}

type reportTrendPayload struct {
	Labels    []string       `json:"labels"`
	Values    []int64        `json:"values"`
	Formatted []string       `json:"formatted"`
	Current   int            `json:"current"`
	Unit      reportBarsUnit `json:"unit"`
}

func reportTrendJSON(d web.LayoutData, key string, trend []reportTrendPeriod, spendBy map[uuid.UUID][]report.CategorySlice, currency money.Currency) (string, []templates.ReportTrendPoint) {
	p := reportTrendPayload{
		Labels: make([]string, 0, len(trend)), Values: make([]int64, 0, len(trend)), Formatted: make([]string, 0, len(trend)),
		Current: -1, Unit: reportBarsUnit{Divisor: reportPow10(currency.Exponent()), Symbol: reportCurrencySymbol(d, currency)},
	}
	points := make([]templates.ReportTrendPoint, 0, len(trend))
	for i, tp := range trend {
		amt := reportAmountOf(spendBy[tp.id], key, currency)
		p.Labels = append(p.Labels, tp.short)
		p.Values = append(p.Values, amt.Minor)
		p.Formatted = append(p.Formatted, templates.Money(d, amt))
		if tp.current {
			p.Current = i
		}
		points = append(points, templates.ReportTrendPoint{Label: tp.label, Amount: amt})
	}
	return templates.ChartJSON(p), points
}

type reportComparePayload struct {
	Categories []string            `json:"categories"`
	Current    []int64             `json:"current"`
	Previous   []int64             `json:"previous"`
	Formatted  map[string][]string `json:"formatted"`
	Labels     map[string]string   `json:"labels"`
	Unit       reportBarsUnit      `json:"unit"`
}

func reportCompareJSON(d web.LayoutData, fold reportFold, prevSpend []report.CategorySlice, cur, prev templates.PeriodLabel, currency money.Currency) string {
	kept := slices.DeleteFunc(slices.Clone(fold.rows), func(s reportSlice) bool { return s.Other })
	if len(kept) < 2 {
		return ""
	}
	p := reportComparePayload{
		Formatted: map[string][]string{"current": {}, "previous": {}},
		Labels:    map[string]string{"current": templates.PeriodLabelText(d, cur), "previous": templates.PeriodLabelText(d, prev)},
		Unit:      reportBarsUnit{Divisor: reportPow10(currency.Exponent()), Symbol: reportCurrencySymbol(d, currency)},
	}
	for _, s := range kept {
		before := reportAmountOf(prevSpend, s.Key, currency)
		p.Categories = append(p.Categories, s.Name)
		p.Current = append(p.Current, s.Amount.Minor)
		p.Previous = append(p.Previous, before.Minor)
		p.Formatted["current"] = append(p.Formatted["current"], templates.Money(d, s.Amount))
		p.Formatted["previous"] = append(p.Formatted["previous"], templates.Money(d, before))
	}
	return templates.ChartJSON(p)
}

func reportSharePercents(rows []reportSlice) []int {
	out := make([]int, len(rows))
	if len(rows) == 0 {
		return out
	}
	remainder := make([]float64, len(rows))
	order := make([]int, len(rows))
	exactTotal, floorTotal := 0.0, 0
	for i, s := range rows {
		exact := max(s.Share, 0) * 100
		floor := math.Floor(exact)
		out[i] = int(floor)
		remainder[i] = exact - floor
		exactTotal += exact
		floorTotal += out[i]
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(remainder[b], remainder[a]) })
	left := int(math.Round(exactTotal)) - floorTotal
	for k := 0; k < left && k < len(order); k++ {
		out[order[k]]++
	}
	return out
}

type reportNames map[string]int

func (n reportNames) unique(name string) string {
	if strings.TrimSpace(name) == "" {
		name = "?"
	}
	if _, taken := n[name]; !taken {
		n[name] = 1
		return name
	}
	for {
		n[name]++
		candidate := name + " (" + strconv.Itoa(n[name]) + ")"
		if _, taken := n[candidate]; !taken {
			n[candidate] = 1
			return candidate
		}
	}
}

type reportDonutItem struct {
	Name      string `json:"name"`
	Value     int64  `json:"value"`
	Formatted string `json:"formatted"`
	Color     string `json:"color"`
}

type reportDonutPayload struct {
	Items []reportDonutItem `json:"items"`
	Empty string            `json:"empty"`
}

func reportDonutJSON(d web.LayoutData, rows []reportSlice) string {
	if len(rows) == 0 {
		return ""
	}
	items := make([]reportDonutItem, 0, len(rows))
	for i, s := range rows {
		items = append(items, reportDonutItem{
			Name: s.Name, Value: s.Amount.Minor, Formatted: templates.Money(d, s.Amount), Color: templates.ReportSlotColor(reportSlot(i, s)).Var,
		})
	}
	return templates.ChartJSON(reportDonutPayload{Items: items, Empty: d.Tr("report.no_data")})
}

type reportBarsUnit struct {
	Divisor int64  `json:"divisor"`
	Symbol  string `json:"symbol"`
}

type reportBarsPayload struct {
	Categories []string            `json:"categories"`
	Full       []string            `json:"full"`
	Income     []int64             `json:"income"`
	Expense    []int64             `json:"expense"`
	Net        []int64             `json:"net"`
	Formatted  map[string][]string `json:"formatted"`
	Labels     map[string]string   `json:"labels"`
	Unit       reportBarsUnit      `json:"unit"`
	Empty      string              `json:"empty"`
}

func reportBars(d web.LayoutData, points []report.CashflowPoint, labels map[uuid.UUID]templates.PeriodLabel, currency money.Currency) (string, []templates.ReportCashflowRow) {
	if !reportHasMovement(points) {
		return "", nil
	}
	p := reportBarsPayload{
		Categories: make([]string, 0, len(points)),
		Full:       make([]string, 0, len(points)),
		Income:     make([]int64, 0, len(points)),
		Expense:    make([]int64, 0, len(points)),
		Net:        make([]int64, 0, len(points)),
		Formatted: map[string][]string{
			"income":  make([]string, 0, len(points)),
			"expense": make([]string, 0, len(points)),
			"net":     make([]string, 0, len(points)),
		},
		Labels: map[string]string{
			"income":  d.Tr("report.money_in"),
			"expense": d.Tr("report.money_out"),
			"net":     d.Tr("overview.net"),
		},
		Unit:  reportBarsUnit{Divisor: reportPow10(currency.Exponent()), Symbol: reportCurrencySymbol(d, currency)},
		Empty: d.Tr("report.no_data"),
	}
	rows := make([]templates.ReportCashflowRow, 0, len(points))
	for _, pt := range points {
		label := templates.PeriodLabelText(d, labels[pt.Period.ID])
		p.Categories = append(p.Categories, templates.PeriodShortLabelText(d, labels[pt.Period.ID]))
		p.Full = append(p.Full, label)
		p.Income = append(p.Income, pt.Income.Minor)
		p.Expense = append(p.Expense, pt.Expense.Minor)
		p.Net = append(p.Net, pt.Net.Minor)
		p.Formatted["income"] = append(p.Formatted["income"], templates.Money(d, pt.Income))
		p.Formatted["expense"] = append(p.Formatted["expense"], templates.Money(d, pt.Expense))
		p.Formatted["net"] = append(p.Formatted["net"], templates.SignedMoney(d, pt.Net))
		rows = append(rows, templates.ReportCashflowRow{Label: label, In: pt.Income, Out: pt.Expense, Net: pt.Net})
	}
	return templates.ChartJSON(p), rows
}

func reportHasMovement(points []report.CashflowPoint) bool {
	for _, pt := range points {
		if pt.Income.Minor != 0 || pt.Expense.Minor != 0 {
			return true
		}
	}
	return false
}

type reportSankeyNode struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type reportSankeyLink struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Value     int64  `json:"value"`
	Formatted string `json:"formatted"`
	Tooltip   string `json:"tooltip"`
}

type reportSankeyPayload struct {
	Nodes []reportSankeyNode `json:"nodes"`
	Links []reportSankeyLink `json:"links"`
	Empty string             `json:"empty"`
}

type reportWalletFlows struct {
	name    string
	amounts map[string]int64
}

type reportSankeyView struct {
	payload string
	rows    int
	flows   []templates.ReportFlowRow
}

func reportSankey(d web.LayoutData, flows []report.Flow, fold reportFold) reportSankeyView {
	order := make([]uuid.UUID, 0, len(flows))
	byWallet := make(map[uuid.UUID]*reportWalletFlows, len(flows))
	targeted := make(map[string]bool, len(fold.rows)+1)
	for _, f := range flows {
		if f.Amount.Minor <= 0 {
			continue
		}
		target, ok := fold.kept[reportCategoryKey(f.CategoryID)]
		if !ok {
			target = fold.other
		}
		w, seen := byWallet[f.WalletID]
		if !seen {
			w = &reportWalletFlows{name: f.WalletName, amounts: map[string]int64{}}
			byWallet[f.WalletID] = w
			order = append(order, f.WalletID)
		}
		w.amounts[target] += f.Amount.Minor
		targeted[target] = true
	}
	if len(order) == 0 {
		return reportSankeyView{}
	}
	colors := make(map[string]string, len(fold.rows))
	for i, s := range fold.rows {
		colors[s.Name] = templates.ReportSlotColor(reportSlot(i, s)).Var
	}

	names := reportNames{}
	targets := make([]string, 0, len(fold.rows)+1)
	for _, s := range fold.rows {
		if targeted[s.Name] {
			targets = append(targets, names.unique(s.Name))
		}
	}
	if targeted[fold.other] && !slices.Contains(targets, fold.other) {
		targets = append(targets, names.unique(fold.other))
	}

	nodes := make([]reportSankeyNode, 0, len(targets)+len(order))
	for _, t := range targets {
		nodes = append(nodes, reportSankeyNode{Name: t, Color: colors[t]})
	}
	currency := flows[0].Amount.Currency
	links := make([]reportSankeyLink, 0, len(order)*len(targets))
	rows := make([]templates.ReportFlowRow, 0, len(order)*len(targets))
	for _, id := range order {
		w := byWallet[id]
		source := names.unique(w.name)
		nodes = append(nodes, reportSankeyNode{Name: source})
		for _, t := range targets {
			minor, ok := w.amounts[t]
			if !ok {
				continue
			}
			amt := money.New(minor, currency)
			formatted := templates.Money(d, amt)
			links = append(links, reportSankeyLink{
				Source: source, Target: t, Value: minor, Formatted: formatted,
				Tooltip: d.Tr("report.flow_tooltip", "Wallet", w.name, "Category", t, "Amount", formatted),
			})
			rows = append(rows, templates.ReportFlowRow{Wallet: w.name, Category: t, Amount: amt})
		}
	}
	if len(links) == 0 {
		return reportSankeyView{}
	}
	payload := templates.ChartJSON(reportSankeyPayload{Nodes: nodes, Links: links, Empty: d.Tr("report.no_data")})
	return reportSankeyView{payload: payload, rows: max(len(order), len(targets)), flows: rows}
}

func reportCurrencySymbol(d web.LayoutData, c money.Currency) string {
	return strings.TrimRight(templates.Money(d, money.Zero(c)), "0123456789.,\u00a0\u202f ")
}

func reportPow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}
