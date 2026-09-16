package handlers

import (
	"net/http"

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

// OverviewHandler owns the project overview: what is left to spend, what moved this period, and quick-add.
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
func (h *OverviewHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/overview", h.GetOverview)
}

// requireProject resolves the org and project the path names, gating membership before any row is read.
func (h *OverviewHandler) requireProject(w http.ResponseWriter, r *http.Request) (projectScope, bool) {
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

// remember stores the org and project as the session's last-used pair, which only / reads.
func (h *OverviewHandler) remember(sc projectScope) {
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

// GetOverview renders the project overview. SECURITY: every read is pure — the first period is created by the first write, never by opening this page.
func (h *OverviewHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	h.remember(sc)

	d := h.LayoutForProject(sc.req, h.title(sc), sc.org.Slug, sc.project, "overview")
	v, err := h.view(sc, d)
	if err != nil {
		h.LogErr("web overview: build", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load project overview.", err)
		return
	}
	Render(w, sc.req, templates.YasakuOverviewLayout(d, v))
}

func (h *OverviewHandler) view(sc projectScope, d web.LayoutData) (templates.YasakuOverviewView, error) {
	cur := h.tx.currency(sc)
	v := templates.YasakuOverviewView{
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Spendable:   money.Zero(cur),
		Total:       money.Zero(cur),
		Income:      money.Zero(cur),
		Expense:     money.Zero(cur),
		Net:         money.Zero(cur),
	}

	lines, err := h.Reports.WalletBalances(sc.req.Context())
	if err != nil {
		return templates.YasakuOverviewView{}, err
	}
	for _, l := range lines {
		v.Wallets = append(v.Wallets, templates.YasakuWalletCard{
			ID:       l.WalletID.String(),
			Name:     l.Name,
			Kind:     l.Kind,
			Balance:  l.Closing,
			Excluded: l.ExcludeFromTotal,
		})
		if l.Closing.Currency != v.Total.Currency {
			continue
		}
		v.Total = v.Total.Add(l.Closing)
		if !l.ExcludeFromTotal {
			v.Spendable = v.Spendable.Add(l.Closing)
		}
	}

	if err := h.fillPeriod(sc, &v); err != nil {
		return templates.YasakuOverviewView{}, err
	}
	if v.Form, err = h.tx.formView(sc, d, txInput{Kind: transaction.KindExpense}, ""); err != nil {
		return templates.YasakuOverviewView{}, err
	}
	v.Form.Recent = true
	if v.List, err = h.tx.recentView(sc, d); err != nil {
		return templates.YasakuOverviewView{}, err
	}
	return v, nil
}

// fillPeriod reads the current period without creating one; a project that has none shows the empty state.
func (h *OverviewHandler) fillPeriod(sc projectScope, v *templates.YasakuOverviewView) error {
	cur, err := h.Periods.Current(sc.req.Context())
	if err != nil {
		if period.IsNotFoundError(err) {
			return nil
		}
		return err
	}
	v.HasPeriod = true
	v.PeriodName = cur.Name

	summary, err := h.Reports.Summary(sc.req.Context(), cur.ID)
	if err != nil {
		return err
	}
	if summary.Currency == "" {
		return nil
	}
	v.Income, v.Expense, v.Net = summary.Income, summary.Expense, summary.Net
	return nil
}

func (h *OverviewHandler) title(sc projectScope) string {
	return h.Base(sc.req, "").Tr("nav.overview") + " · " + sc.project.Name
}
