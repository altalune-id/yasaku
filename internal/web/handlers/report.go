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
func (h *ReportHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/reports", h.GetReports)
}

func (h *ReportHandler) requireProject(w http.ResponseWriter, r *http.Request) (projectScope, bool) {
	p, sid, ok := h.LoadSession(r)
	if !ok {
		http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return projectScope{}, false
	}
	o, r, ok := h.OrgScopeFor(w, r, p, r.PathValue("org"))
	if !ok {
		return projectScope{}, false
	}
	proj, r, ok := h.ProjectScopeFor(w, r, o.ID, r.PathValue("project"))
	if !ok {
		return projectScope{}, false
	}
	return projectScope{principal: p, sid: sid, org: o, project: proj, req: r}, true
}

// GetReports renders the summary table and the three charts for one period.
func (h *ReportHandler) GetReports(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, "reports")
	d.Title = d.Tr("report.title") + " · " + sc.project.Name
	v := templates.ReportsView{
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
	}

	recent, err := h.Periods.List(sc.req.Context(), period.ListOpts{Limit: reportPeriodWindow})
	if err != nil {
		h.reportFailed(w, sc, "list periods", err)
		return
	}
	selected, ok := h.selectPeriod(w, sc, recent)
	if !ok {
		return
	}
	if selected == nil {
		v.NoPeriod = true
		Render(w, sc.req, templates.ReportsLayout(d, v))
		return
	}

	window, err := h.cashflowWindow(sc, recent, selected)
	if err != nil {
		h.reportFailed(w, sc, "cashflow window", err)
		return
	}
	v.Periods = reportPeriodOptions(recent, selected)
	v.PeriodName = selected.Name
	if !h.fill(w, sc, d, selected, window, &v) {
		return
	}
	Render(w, sc.req, templates.ReportsLayout(d, v))
}

// selectPeriod resolves ?period=, falling back to the current period and then to the newest in scope. SECURITY: ByID is scope-checked, so a foreign period id renders as a 404 rather than another project's totals.
func (h *ReportHandler) selectPeriod(w http.ResponseWriter, sc projectScope, recent []*period.Period) (*period.Period, bool) {
	if raw := strings.TrimSpace(sc.req.URL.Query().Get("period")); raw != "" {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad period",
				"That link carries a malformed period id.", parseErr)
			return nil, false
		}
		p, err := h.Periods.ByID(sc.req.Context(), id)
		if err == nil {
			return p, true
		}
		if period.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, "Period not found", "No period with that id in this project.", err)
			return nil, false
		}
		h.reportFailed(w, sc, "period byID", err)
		return nil, false
	}
	cur, err := h.Periods.Current(sc.req.Context())
	if err == nil {
		return cur, true
	}
	if !period.IsNotFoundError(err) {
		h.reportFailed(w, sc, "current period", err)
		return nil, false
	}
	if len(recent) == 0 {
		return nil, true
	}
	return recent[0], true
}

func (h *ReportHandler) cashflowWindow(sc projectScope, recent []*period.Period, selected *period.Period) ([]*period.Period, error) {
	if len(recent) > 0 && recent[0].ID == selected.ID {
		return recent, nil
	}
	before := selected.StartDate.AddDays(1)
	return h.Periods.List(sc.req.Context(), period.ListOpts{Limit: reportPeriodWindow, Before: &before})
}

func (h *ReportHandler) fill(
	w http.ResponseWriter, sc projectScope, d web.LayoutData,
	selected *period.Period, window []*period.Period, v *templates.ReportsView,
) bool {
	ctx := sc.req.Context()
	summary, err := h.Reports.Summary(ctx, selected.ID)
	if err != nil {
		h.reportFailed(w, sc, "summary", err)
		return false
	}
	spend, err := h.Reports.SpendByCategory(ctx, selected.ID)
	if err != nil {
		h.reportFailed(w, sc, "spend by category", err)
		return false
	}
	income, err := h.Reports.IncomeByCategory(ctx, selected.ID)
	if err != nil {
		h.reportFailed(w, sc, "income by category", err)
		return false
	}
	points, err := h.Reports.Cashflow(ctx, reportCashflowIDs(window))
	if err != nil {
		h.reportFailed(w, sc, "cashflow", err)
		return false
	}
	flows, err := h.Reports.Flows(ctx, selected.ID)
	if err != nil {
		h.reportFailed(w, sc, "flows", err)
		return false
	}

	currency := summary.Currency
	spendFold := reportFoldSlices(d, spend, currency)
	incomeFold := reportFoldSlices(d, income, currency)

	v.Income = templates.Money(d, summary.Income)
	v.Expense = templates.Money(d, summary.Expense)
	v.Net = templates.SignedMoney(d, summary.Net)
	v.TxCount = summary.TxCount
	v.Wallets, v.Totals = reportWalletRows(d, summary)
	v.TopSpend = reportCategoryRows(d, spendFold.rows)
	v.TopIncome = reportCategoryRows(d, incomeFold.rows)
	v.Donut = reportDonutJSON(d, spendFold.rows)
	v.Bars = reportBarsJSON(d, points, currency)
	v.Sankey = reportSankeyJSON(d, flows, spendFold)
	return true
}

func (h *ReportHandler) reportFailed(w http.ResponseWriter, sc projectScope, what string, err error) {
	h.LogErr("web report: "+what, err)
	h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Report failed", "Could not load this report.", err)
}

func reportCashflowIDs(window []*period.Period) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(window))
	for i := len(window) - 1; i >= 0; i-- {
		ids = append(ids, window[i].ID)
	}
	return ids
}

func reportPeriodOptions(recent []*period.Period, selected *period.Period) []templates.ReportPeriodOption {
	opts := make([]templates.ReportPeriodOption, 0, len(recent)+1)
	found := false
	for _, p := range recent {
		if p.ID == selected.ID {
			found = true
		}
		opts = append(opts, templates.ReportPeriodOption{
			ID: p.ID.String(), Name: p.Name, Selected: p.ID == selected.ID,
		})
	}
	if found {
		return opts
	}
	return append([]templates.ReportPeriodOption{
		{ID: selected.ID.String(), Name: selected.Name, Selected: true},
	}, opts...)
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
		f.kept[reportCategoryKey(s.CategoryID)] = label
		f.rows = append(f.rows, reportSlice{Name: label, Amount: s.Amount, Share: s.Share})
	}
	f.other = names.unique(d.Tr(reportOtherKey))
	if other.Minor > 0 {
		f.rows = append(f.rows, reportSlice{Name: f.other, Amount: other, Share: otherShare})
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
	for _, s := range rows {
		items = append(items, reportDonutItem{
			Name: s.Name, Value: s.Amount.Minor, Formatted: templates.Money(d, s.Amount),
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
	Income     []int64             `json:"income"`
	Expense    []int64             `json:"expense"`
	Net        []int64             `json:"net"`
	Formatted  map[string][]string `json:"formatted"`
	Labels     map[string]string   `json:"labels"`
	Unit       reportBarsUnit      `json:"unit"`
	Empty      string              `json:"empty"`
}

func reportBarsJSON(d web.LayoutData, points []report.CashflowPoint, currency money.Currency) string {
	if !reportHasMovement(points) {
		return ""
	}
	p := reportBarsPayload{
		Categories: make([]string, 0, len(points)),
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
	for _, pt := range points {
		label := strings.TrimSpace(pt.Period.Name)
		if label == "" {
			label = pt.Period.Start.String()
		}
		p.Categories = append(p.Categories, label)
		p.Income = append(p.Income, pt.Income.Minor)
		p.Expense = append(p.Expense, pt.Expense.Minor)
		p.Net = append(p.Net, pt.Net.Minor)
		p.Formatted["income"] = append(p.Formatted["income"], templates.Money(d, pt.Income))
		p.Formatted["expense"] = append(p.Formatted["expense"], templates.Money(d, pt.Expense))
		p.Formatted["net"] = append(p.Formatted["net"], templates.SignedMoney(d, pt.Net))
	}
	return templates.ChartJSON(p)
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
	Name string `json:"name"`
}

type reportSankeyLink struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Value     int64  `json:"value"`
	Formatted string `json:"formatted"`
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

func reportSankeyJSON(d web.LayoutData, flows []report.Flow, fold reportFold) string {
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
		return ""
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
		nodes = append(nodes, reportSankeyNode{Name: t})
	}
	currency := flows[0].Amount.Currency
	links := make([]reportSankeyLink, 0, len(order)*len(targets))
	for _, id := range order {
		w := byWallet[id]
		source := names.unique(w.name)
		nodes = append(nodes, reportSankeyNode{Name: source})
		for _, t := range targets {
			minor, ok := w.amounts[t]
			if !ok {
				continue
			}
			links = append(links, reportSankeyLink{
				Source: source, Target: t, Value: minor,
				Formatted: templates.Money(d, money.New(minor, currency)),
			})
		}
	}
	if len(links) == 0 {
		return ""
	}
	return templates.ChartJSON(reportSankeyPayload{Nodes: nodes, Links: links, Empty: d.Tr("report.no_data")})
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
