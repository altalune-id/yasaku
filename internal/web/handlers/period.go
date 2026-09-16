package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

const periodNavKey = "periods"

// PeriodHandler serves the "tutup buku" close-book screens for one project.
type PeriodHandler struct {
	Deps
	Periods *period.Service
	Reports *report.Service
	Ledgers *ledger.Service
}

// NewPeriodHandler wires the handler.
func NewPeriodHandler(d Deps, projects *project.Service, periods *period.Service, reports *report.Service, ledgers *ledger.Service) *PeriodHandler {
	d.Projects = projects
	return &PeriodHandler{Deps: d, Periods: periods, Reports: reports, Ledgers: ledgers}
}

// Register wires the period routes onto mux.
func (h *PeriodHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/periods", h.GetPeriods)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/periods/{id}/close", h.GetClose)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/periods/{id}/close", h.PostClose)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/periods/{id}/reopen", h.PostReopen)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/periods/{id}/rename", h.PostRename)
}

// GetPeriods renders the current period card and the closed-period history.
func (h *PeriodHandler) GetPeriods(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	h.renderPeriods(w, sc, nil)
}

// GetClose renders the close confirmation page, or just its preview for an HTMX date change.
func (h *PeriodHandler) GetClose(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePeriod(w, r)
	if !ok {
		return
	}
	v := h.closeView(sc, p, h.requestedEnd(sc.req, p))
	d := h.layout(sc)
	if h.isHTMX(sc.req) {
		Render(w, sc.req, templates.ClosePreviewFragment(d, v))
		return
	}
	d.Title = d.Tr("period.close") + " · " + sc.project.Name
	Render(w, sc.req, templates.ClosePeriodLayout(d, v))
}

// PostClose freezes the period's totals and opens the next one.
func (h *PeriodHandler) PostClose(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePeriod(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	end, err := civil.ParseDate(strings.TrimSpace(sc.req.PostForm.Get("end_date")))
	if err != nil {
		h.renderClose(w, sc, p, h.requestedEnd(sc.req, p), &period.InvalidRangeError{Reason: "end date is not a date"})
		return
	}
	if _, closeErr := h.Periods.Close(sc.req.Context(), p.ID, end, sc.principal.UserID); closeErr != nil {
		h.closeFailed(w, sc, p, end, closeErr)
		return
	}
	h.redirectToPeriods(w, sc)
}

// PostReopen unlocks the most recently closed period.
func (h *PeriodHandler) PostReopen(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePeriod(w, r)
	if !ok {
		return
	}
	if _, err := h.Periods.Reopen(sc.req.Context(), p.ID); err != nil {
		if !isPeriodDomainError(err) {
			h.LogErr("web period: reopen", err)
			h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Reopen failed", "Could not reopen that period.", err)
			return
		}
		h.renderPeriods(w, sc, err)
		return
	}
	h.redirectToPeriods(w, sc)
}

// PostRename replaces the period's display name.
func (h *PeriodHandler) PostRename(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePeriod(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(sc.req.PostForm.Get("name"))
	if _, err := h.Periods.Rename(sc.req.Context(), p.ID, name); err != nil {
		if !isPeriodDomainError(err) {
			h.LogErr("web period: rename", err)
			h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Rename failed", "Could not rename that period.", err)
			return
		}
		h.renderPeriods(w, sc, err)
		return
	}
	h.redirectToPeriods(w, sc)
}

// requireProject resolves the org and project the path names, gating membership before any row is read.
func (h *PeriodHandler) requireProject(w http.ResponseWriter, r *http.Request) (projectScope, bool) {
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

func (h *PeriodHandler) requirePeriod(w http.ResponseWriter, r *http.Request) (projectScope, *period.Period, bool) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return projectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That period no longer exists.")
		return projectScope{}, nil, false
	}
	p, err := h.Periods.ByID(sc.req.Context(), id)
	if err != nil {
		if period.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That period no longer exists.", err)
			return projectScope{}, nil, false
		}
		h.LogErr("web period: byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Lookup failed", "Could not load that period.", err)
		return projectScope{}, nil, false
	}
	return sc, p, true
}

func (h *PeriodHandler) layout(sc projectScope) web.LayoutData {
	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, periodNavKey)
	d.Title = d.Tr("period.title") + " · " + sc.project.Name
	return d
}

func (h *PeriodHandler) isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func (h *PeriodHandler) redirectToPeriods(w http.ResponseWriter, sc projectScope) {
	target := web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, "/periods"))
	//nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns.
	http.Redirect(w, sc.req, target, http.StatusSeeOther)
}

func (h *PeriodHandler) renderPeriods(w http.ResponseWriter, sc projectScope, cause error) {
	v, err := h.periodsView(sc, cause)
	if err != nil {
		h.LogErr("web period: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load periods.", err)
		return
	}
	Render(w, sc.req, templates.PeriodsLayout(h.layout(sc), v))
}

func (h *PeriodHandler) renderClose(w http.ResponseWriter, sc projectScope, p *period.Period, end civil.Date, cause error) {
	v := h.closeView(sc, p, end)
	if cause != nil {
		v.Error, v.ErrorCode = appErrorBanner(cause)
	}
	d := h.layout(sc)
	d.Title = d.Tr("period.close") + " · " + sc.project.Name
	Render(w, sc.req, templates.ClosePeriodLayout(d, v))
}

// closeFailed maps a refused close onto page state: an already-closed period is a state, not an error.
func (h *PeriodHandler) closeFailed(w http.ResponseWriter, sc projectScope, p *period.Period, end civil.Date, err error) {
	if period.IsAlreadyClosedError(err) {
		fresh, freshErr := h.Periods.ByID(sc.req.Context(), p.ID)
		if freshErr != nil {
			h.LogErr("web period: reload after already-closed", freshErr)
			fresh = p
		}
		h.renderClose(w, sc, fresh, end, nil)
		return
	}
	if !isPeriodDomainError(err) {
		h.LogErr("web period: close", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Close failed", "Could not close that period.", err)
		return
	}
	h.renderClose(w, sc, p, end, err)
}

func (h *PeriodHandler) periodsView(sc projectScope, cause error) (templates.PeriodsView, error) {
	ctx := sc.req.Context()
	items, err := h.Periods.List(ctx, period.ListOpts{})
	if err != nil {
		return templates.PeriodsView{}, err
	}
	v := templates.PeriodsView{
		OrgSlug:     sc.org.Slug,
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		History:     make([]templates.PeriodRow, 0, len(items)),
	}
	v.Error, v.ErrorCode = appErrorBanner(cause)
	loc := h.location(ctx)
	reopenable := periodReopenableID(items)
	for _, p := range items {
		row := templates.PeriodRow{
			ID:    p.ID.String(),
			Name:  p.Name,
			Start: p.StartDate.String(),
		}
		if p.IsCurrent() {
			v.HasCurrent = true
			v.Current = h.currentRow(ctx, p, row)
			continue
		}
		row.End = p.EndDate.String()
		row.Closed = p.IsLocked()
		row.CanReopen = p.ID == reopenable
		row.CanClose = !p.IsLocked()
		if p.IsLocked() {
			if p.ClosedAt != nil {
				row.ClosedAt = p.ClosedAt.In(loc).Format("2006-01-02 15:04")
			}
			applySnapshot(&row, p.Snapshot)
		}
		v.History = append(v.History, row)
	}
	return v, nil
}

func (h *PeriodHandler) currentRow(ctx context.Context, p *period.Period, row templates.PeriodRow) templates.PeriodRow {
	sum, err := h.Reports.Summary(ctx, p.ID)
	if err != nil {
		h.LogErr("web period: summary", err)
		return row
	}
	row.HasTotals = true
	row.Income, row.Expense, row.Net, row.TxCount = sum.Income, sum.Expense, sum.Net, sum.TxCount
	return row
}

// requestedEnd picks the date the preview is computed for: the frozen one after a reopen, else the query, else today.
func (h *PeriodHandler) requestedEnd(r *http.Request, p *period.Period) civil.Date {
	if p.EndDate != nil {
		return *p.EndDate
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("end_date")); raw != "" {
		if d, err := civil.ParseDate(raw); err == nil {
			return d
		}
	}
	return civil.DateOf(time.Now(), h.location(r.Context()))
}

func (h *PeriodHandler) closeView(sc projectScope, p *period.Period, end civil.Date) templates.ClosePeriodView {
	ctx := sc.req.Context()
	v := templates.ClosePeriodView{
		OrgSlug:       sc.org.Slug,
		ProjectSlug:   sc.project.Slug,
		ProjectName:   sc.project.Name,
		PeriodID:      p.ID.String(),
		PeriodName:    p.Name,
		Start:         p.StartDate.String(),
		EndDate:       end.String(),
		MaxDate:       civil.DateOf(time.Now(), h.location(ctx)).String(),
		DateLocked:    p.EndDate != nil,
		AlreadyClosed: p.IsLocked(),
	}
	if p.IsLocked() {
		v.Preview, v.HasPreview = previewFromSnapshot(p.Snapshot)
		return v
	}
	snap, err := h.Periods.PreviewClose(ctx, p.ID, end)
	if err != nil {
		if !isPeriodDomainError(err) {
			h.LogErr("web period: preview close", err)
		}
		v.Error, v.ErrorCode = appErrorBanner(err)
		return v
	}
	v.Preview, v.HasPreview = previewFromSnapshot(&snap)
	return v
}

func (h *PeriodHandler) location(ctx context.Context) *time.Location {
	st, err := h.Ledgers.Get(ctx)
	if err != nil {
		h.LogErr("web period: settings", err)
		return time.UTC
	}
	loc, err := st.Location()
	if err != nil || loc == nil {
		return time.UTC
	}
	return loc
}

// periodReopenableID returns the one period period.Service.Reopen would accept, or uuid.Nil when none would.
func periodReopenableID(items []*period.Period) uuid.UUID {
	for _, p := range items {
		if p.IsCurrent() || !p.IsLocked() {
			continue
		}
		latest := true
		for _, o := range items {
			if o.ID == p.ID || o.IsCurrent() {
				continue
			}
			if !o.IsLocked() || !o.StartDate.Before(p.StartDate) {
				latest = false
				break
			}
		}
		if latest {
			return p.ID
		}
	}
	return uuid.Nil
}

func applySnapshot(row *templates.PeriodRow, s *period.Snapshot) {
	if s == nil {
		return
	}
	row.HasTotals = true
	row.Income = money.New(s.Income, s.Currency)
	row.Expense = money.New(s.Expense, s.Currency)
	row.Net = money.New(s.Net, s.Currency)
	row.TxCount = s.TxCount
}

func previewFromSnapshot(s *period.Snapshot) (templates.ClosePreview, bool) {
	if s == nil {
		return templates.ClosePreview{}, false
	}
	p := templates.ClosePreview{
		Income:  money.New(s.Income, s.Currency),
		Expense: money.New(s.Expense, s.Currency),
		Net:     money.New(s.Net, s.Currency),
		TxCount: s.TxCount,
		Wallets: make([]templates.CloseWalletRow, 0, len(s.Wallets)),
	}
	for _, w := range s.Wallets {
		p.Wallets = append(p.Wallets, templates.CloseWalletRow{Name: w.Name, Closing: money.New(w.Closing, s.Currency)})
	}
	return p, true
}

// appErrorBanner renders a typed domain error as banner copy plus its code. Codes: docs/ERROR_CODES.md.
func appErrorBanner(err error) (msg, code string) { //nolint:nonamedreturns // two strings differ only by role
	if err == nil {
		return "", ""
	}
	ae, ok := apperror.AsAppError(err)
	if !ok {
		return "", ""
	}
	return ae.Message(), ae.Code()
}

func isPeriodDomainError(err error) bool {
	return period.IsNotFoundError(err) || period.IsInvalidNameError(err) ||
		period.IsInvalidRangeError(err) || period.IsOverlapError(err) ||
		period.IsAlreadyClosedError(err) || period.IsNotClosedError(err) ||
		period.IsNotLatestClosedError(err)
}
