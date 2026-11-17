package handlers

import (
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

// OverviewHandler owns Home: one period's totals, quick add and entries grouped by day.
type OverviewHandler struct {
	Deps
	Wallets      *wallet.Service
	Transactions *transaction.Service
	Periods      *period.Service
	Reports      *report.Service
	TxCategories *category.Service
	Ledgers      *ledger.Service

	tx *TransactionHandler
}

const (
	homeQuickAddCount  = 4
	homeQuickAddScan   = 12
	homeQuickAddWindow = 90 * 24 * time.Hour
)

// NewOverviewHandler wires the handler.
func NewOverviewHandler(
	d Deps,
	projects *project.Service,
	wallets *wallet.Service,
	transactions *transaction.Service,
	periods *period.Service,
	reports *report.Service,
	categories *category.Service,
	ledgers *ledger.Service,
) *OverviewHandler {
	d.Projects = projects
	return &OverviewHandler{
		Deps:         d,
		Wallets:      wallets,
		Transactions: transactions,
		Periods:      periods,
		Reports:      reports,
		TxCategories: categories,
		Ledgers:      ledgers,
		tx:           NewTransactionHandler(d, projects, wallets, transactions, periods, categories, ledgers),
	}
}

// Register wires the overview route onto mux.
func (h *OverviewHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/overview", h.GetOverview)
}

func (h *OverviewHandler) remember(sc ProjectScope) {
	if sc.principal.ActiveOrgID == sc.org.ID && sc.principal.ActiveProjectID == sc.project.ID {
		return
	}
	updated := sc.principal
	updated.ActiveOrgID = sc.org.ID
	updated.ActiveProjectID = sc.project.ID
	if err := h.UpdateSession(sc.req, sc.sid, updated); err != nil {
		h.LogErr("web overview: update session", err)
	}
}

// GetOverview renders Home for one period. SECURITY: every read is pure — the first period is created by the first write, never by opening this page.
func (h *OverviewHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	h.remember(sc)

	d := h.LayoutForProject(sc.req, h.title(sc), sc.org.Slug, sc.project, "overview")
	wallets, err := h.Wallets.List(sc.req.Context(), wallet.ListOpts{IncludeArchived: true})
	if err != nil {
		h.renderFailure(w, sc, d, err)
		return
	}
	first := needsFirstRun(wallets)
	if first && h.CanManage(sc) {
		target := web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, "/first-run"))
		http.Redirect(w, sc.req, target, http.StatusSeeOther) //nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns
		return
	}
	v, err := h.view(sc, d, wallets)
	if err != nil {
		h.renderFailure(w, sc, d, err)
		return
	}
	v.SetupPending = first
	v.Toast = h.tx.takeTxToast(w, sc, d)
	d.AddPulse = v.Entries == 0 && !v.Closed && !v.NoWallet && !v.SetupPending && v.NextPeriodID == ""
	Render(w, sc.req, templates.YasakuOverviewLayout(d, v))
}

func (h *OverviewHandler) renderFailure(w http.ResponseWriter, sc ProjectScope, d web.LayoutData, err error) {
	h.LogErr("web overview: build", err)
	v := templates.YasakuOverviewView{ProjectSlug: sc.project.Slug, Failed: true}
	RenderStatus(w, sc.req, http.StatusInternalServerError, templates.YasakuOverviewLayout(d, v))
}

func (h *OverviewHandler) view(sc ProjectScope, d web.LayoutData, wallets []*wallet.Wallet) (templates.YasakuOverviewView, error) {
	ctx := sc.req.Context()
	cur := h.tx.currency(sc)
	v := templates.YasakuOverviewView{
		ProjectSlug: sc.project.Slug,
		Income:      money.Zero(cur),
		Expense:     money.Zero(cur),
		Net:         money.Zero(cur),
	}
	if !slices.ContainsFunc(wallets, func(w *wallet.Wallet) bool { return !w.IsArchived() }) {
		v.NoWallet = true
		return v, nil
	}
	p, current, err := h.selectedPeriod(sc)
	if err != nil || p == nil {
		return v, err
	}
	v.HasPeriod, v.PeriodID, v.PeriodName, v.Closed = true, p.ID.String(), p.Name, p.Status == period.StatusClosed
	v.PeriodStart, v.PeriodMonthly = p.StartDate, p.Name == period.DefaultName(p.StartDate)
	if err := h.fillNeighbors(sc, p.ID, &v); err != nil {
		return v, err
	}
	summary, err := h.Reports.Summary(ctx, p.ID)
	if err != nil {
		return v, err
	}
	if summary.Currency != "" {
		v.Income, v.Expense, v.Net, v.Adjustments = summary.Income, summary.Expense, summary.Net, summary.Adjustments
	}
	if snap := p.Snapshot; v.Closed && snap != nil {
		v.Income, v.Expense, v.Net = money.New(snap.Income, snap.Currency), money.New(snap.Expense, snap.Currency), money.New(snap.Net, snap.Currency)
		v.Adjustments = money.New(snap.Adjustments, snap.Currency)
	}
	items, more, err := h.Transactions.List(ctx, transaction.ListOpts{PeriodID: &p.ID, Limit: transaction.MaxListLimit})
	if err != nil {
		return v, err
	}
	rows, err := h.tx.rows(sc, d, items)
	if err != nil {
		return v, err
	}
	today := civil.DateOf(time.Now(), h.tx.location(sc))
	v.Days, v.Entries, v.More = homeDays(rows, today, cur), len(rows), more != nil
	if v.Closed {
		return v, nil
	}
	if v.QuickAdd, err = h.quickAdd(sc); err != nil {
		return v, err
	}
	if !current {
		return v, nil
	}
	end, err := h.Periods.CycleEnd(ctx, p.ID)
	if err != nil {
		return v, err
	}
	v.Ended = end.Before(today)
	return v, nil
}

func (h *OverviewHandler) selectedPeriod(sc ProjectScope) (*period.Period, bool, error) {
	ctx := sc.req.Context()
	cur, err := h.Periods.Current(ctx)
	if err != nil && !period.IsNotFoundError(err) {
		return nil, false, err
	}
	if cur == nil {
		return nil, false, nil
	}
	id, ok := periodParam(sc.req.URL.Query().Get("period"))
	if !ok || id == cur.ID {
		return cur, true, nil
	}
	p, err := h.Periods.ByID(ctx, id)
	if period.IsNotFoundError(err) {
		return cur, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return p, false, nil
}

func (h *OverviewHandler) fillNeighbors(sc ProjectScope, id uuid.UUID, v *templates.YasakuOverviewView) error {
	prev, next, err := h.Periods.Neighbors(sc.req.Context(), id)
	if err != nil {
		return err
	}
	if prev != nil {
		v.PrevPeriodID = prev.ID.String()
	}
	if next != nil {
		v.NextPeriodID = next.ID.String()
	}
	return nil
}

func (h *OverviewHandler) quickAdd(sc ProjectScope) ([]templates.YasakuQuickAdd, error) {
	top, err := h.Transactions.TopCategories(sc.req.Context(), time.Now().Add(-homeQuickAddWindow), homeQuickAddScan)
	if err != nil || len(top) == 0 {
		return nil, err
	}
	cats, err := h.tx.categoriesByID(sc)
	if err != nil {
		return nil, err
	}
	out := make([]templates.YasakuQuickAdd, 0, homeQuickAddCount)
	for _, t := range top {
		c, ok := cats[t.CategoryID]
		if !ok || c.Kind != category.KindExpense || c.IsArchived() {
			continue
		}
		out = append(out, templates.YasakuQuickAdd{ID: c.ID.String(), Name: c.Name, Icon: c.Icon})
		if len(out) == homeQuickAddCount {
			break
		}
	}
	return out, nil
}

func homeDays(rows []templates.TxRow, today civil.Date, cur money.Currency) []templates.YasakuDay {
	var out []templates.YasakuDay
	for _, row := range rows {
		if len(out) == 0 || out[len(out)-1].Day != row.Day {
			out = append(out, templates.YasakuDay{
				Day:       row.Day,
				Today:     row.Day == today,
				Yesterday: row.Day == today.AddDays(-1),
				Total:     money.Zero(cur),
			})
		}
		day := &out[len(out)-1]
		row.Grouped = true
		day.Rows = append(day.Rows, row)
		if !row.Amount.SameCurrency(day.Total) {
			continue
		}
		switch row.Kind {
		case string(transaction.KindIncome):
			day.Total = day.Total.Add(row.Amount)
		case string(transaction.KindExpense):
			day.Total = day.Total.Sub(row.Amount)
		}
	}
	return out
}

func (h *OverviewHandler) title(sc ProjectScope) string {
	return pageTitle(sc.req, "nav.home", sc.project.Name)
}

func periodParam(raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	return id, err == nil
}
