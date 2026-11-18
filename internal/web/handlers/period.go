package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

//i18n:use period.manager_only
//i18n:use period.rename_manager_only
const (
	periodNavKey            = "periods"
	periodManagerOnly       = "Only an owner or admin can close or reopen a period."
	periodRenameManagerOnly = "Only an owner or admin can rename a period."
)

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
func (h *PeriodHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/periods", h.GetPeriods)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/periods/{id}/close", h.GetClose)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/periods/{id}/close", h.PostClose)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/periods/{id}/reopen", h.PostReopen)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/periods/{id}/rename", h.PostRename)
}

// GetPeriods renders the current period card and the closed-period history.
func (h *PeriodHandler) GetPeriods(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
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
	if !h.CanManage(sc) {
		h.forbidNonManager(w, sc.req, "period.manager_only", periodManagerOnly)
		return
	}
	v := h.closeView(sc, p, h.requestedEnd(sc, p))
	d := h.layout(sc)
	if web.IsHTMXRequest(sc.req) {
		Render(w, sc.req, templates.ClosePreviewFragment(d, v))
		return
	}
	d.Title = pageTitle(sc.req, "title.period_close", sc.project.Name)
	Render(w, sc.req, templates.ClosePeriodLayout(d, v))
}

// PostClose freezes the period's totals and opens the next one.
func (h *PeriodHandler) PostClose(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePeriod(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	end, err := period.ParseEnd(sc.req.PostForm.Get("end_date"))
	if err != nil {
		h.renderClose(w, sc, p, h.requestedEnd(sc, p), err)
		return
	}
	if _, closeErr := h.Periods.Close(sc.req.Context(), p.ID, end, sc.principal.UserID); closeErr != nil {
		if h.refuseNotManager(w, sc.req, closeErr, "period.manager_only", periodManagerOnly) {
			return
		}
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
		if h.refuseNotManager(w, sc.req, err, "period.manager_only", periodManagerOnly) {
			return
		}
		if !isPeriodDomainError(err) {
			h.LogErr("web period: reopen", err)
			h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.reopen_failed"), "Could not reopen that period.", err)
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
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(sc.req.PostForm.Get("name"))
	if _, err := h.Periods.Rename(sc.req.Context(), p.ID, name); err != nil {
		if h.refuseNotManager(w, sc.req, err, "period.rename_manager_only", periodRenameManagerOnly) {
			return
		}
		if !isPeriodDomainError(err) {
			h.LogErr("web period: rename", err)
			h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.rename_failed"), "Could not rename that period.", err)
			return
		}
		h.renderPeriods(w, sc, err)
		return
	}
	h.redirectToPeriods(w, sc)
}

func (h *PeriodHandler) requirePeriod(w http.ResponseWriter, r *http.Request) (ProjectScope, *period.Period, bool) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return ProjectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusNotFound, pageTitle(sc.req, "error_title.not_found"), "That period no longer exists.")
		return ProjectScope{}, nil, false
	}
	p, err := h.Periods.ByID(sc.req.Context(), id)
	if err != nil {
		if period.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, pageTitle(sc.req, "error_title.not_found"), "That period no longer exists.", err)
			return ProjectScope{}, nil, false
		}
		h.LogErr("web period: byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.lookup_failed"), "Could not load that period.", err)
		return ProjectScope{}, nil, false
	}
	return sc, p, true
}

func (h *PeriodHandler) layout(sc ProjectScope) web.LayoutData {
	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, periodNavKey)
	d.Title = pageTitle(sc.req, "nav.periods", sc.project.Name)
	return d
}

func (h *PeriodHandler) redirectToPeriods(w http.ResponseWriter, sc ProjectScope) {
	target := web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, "/periods"))
	//nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns.
	http.Redirect(w, sc.req, target, http.StatusSeeOther)
}

func (h *PeriodHandler) renderPeriods(w http.ResponseWriter, sc ProjectScope, cause error) {
	v, err := h.periodsView(sc, cause)
	if err != nil {
		h.LogErr("web period: list", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.list_failed"), "Could not load periods.", err)
		return
	}
	Render(w, sc.req, templates.PeriodsLayout(h.layout(sc), v))
}

func (h *PeriodHandler) renderClose(w http.ResponseWriter, sc ProjectScope, p *period.Period, end civil.Date, cause error) {
	v := h.closeView(sc, p, end)
	if cause != nil {
		v.Error, v.ErrorCode = appErrorBanner(cause)
	}
	d := h.layout(sc)
	d.Title = pageTitle(sc.req, "title.period_close", sc.project.Name)
	Render(w, sc.req, templates.ClosePeriodLayout(d, v))
}

func (h *PeriodHandler) closeFailed(w http.ResponseWriter, sc ProjectScope, p *period.Period, end civil.Date, err error) {
	if period.IsAlreadyClosedError(err) {
		fresh, freshErr := h.Periods.ByID(sc.req.Context(), p.ID)
		if freshErr != nil {
			h.LogErr("web period: reload after already-closed", freshErr)
			fresh = p
		}
		h.renderClose(w, sc, fresh, end, nil)
		return
	}
	if period.IsEntriesAfterEndError(err) {
		d := h.layout(sc)
		d.Title = pageTitle(sc.req, "title.period_close", sc.project.Name)
		RenderStatus(w, sc.req, http.StatusUnprocessableEntity, templates.ClosePeriodLayout(d, h.closeView(sc, p, end)))
		return
	}
	if !isPeriodDomainError(err) {
		h.LogErr("web period: close", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.close_failed"), "Could not close that period.", err)
		return
	}
	h.renderClose(w, sc, p, end, err)
}

func (h *PeriodHandler) periodsView(sc ProjectScope, cause error) (templates.PeriodsView, error) {
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
	reopenable, err := h.Periods.Reopenable(ctx)
	if err != nil {
		return templates.PeriodsView{}, err
	}
	labels := labelsOf(items)
	for _, p := range items {
		row := templates.PeriodRow{
			ID:    p.ID.String(),
			Name:  p.Name,
			Start: p.StartDate,
			Label: labels[p.ID],
		}
		if p.IsCurrent() {
			v.HasCurrent = true
			v.Current = h.currentRow(ctx, p, row)
			h.closeDialog(ctx, p, &v)
			continue
		}
		row.End = p.EndDate
		row.Closed = p.IsLocked()
		row.CanReopen = reopenable != nil && reopenable.ID == p.ID
		row.CanClose = !p.IsLocked()
		if p.IsLocked() {
			row.ClosedAt = p.ClosedAt
			applySnapshot(&row, p.Snapshot)
		}
		v.History = append(v.History, row)
	}
	return v, nil
}

func (h *PeriodHandler) closeDialog(ctx context.Context, p *period.Period, v *templates.PeriodsView) {
	end, err := h.Periods.SuggestedEnd(ctx, p.ID)
	if err != nil {
		h.LogErr("web period: suggested end", err)
		return
	}
	snap, err := h.Periods.PreviewClose(ctx, p.ID, end)
	if err != nil {
		if !isPeriodDomainError(err) {
			h.LogErr("web period: preview close", err)
		}
		return
	}
	v.CloseEnd, v.CloseEndDay = end.String(), end
	v.CloseEndToday = end == h.today(ctx)
	v.Close, v.HasClose = previewFromSnapshot(&snap)
}

func (h *PeriodHandler) currentRow(ctx context.Context, p *period.Period, row templates.PeriodRow) templates.PeriodRow {
	sum, err := h.Reports.Summary(ctx, p.ID)
	if err != nil {
		h.LogErr("web period: summary", err)
		return row
	}
	row.HasTotals = true
	row.Income, row.Expense, row.Net, row.Adjust, row.Openings, row.TxCount = sum.Income, sum.Expense, sum.Net, sum.Adjustments, sum.Openings, sum.TxCount
	return row
}

func (h *PeriodHandler) requestedEnd(sc ProjectScope, p *period.Period) civil.Date {
	ctx := sc.req.Context()
	if p.EndDate == nil {
		if raw := strings.TrimSpace(sc.req.URL.Query().Get("end_date")); raw != "" {
			if d, err := civil.ParseDate(raw); err == nil {
				return d
			}
		}
	}
	end, err := h.Periods.SuggestedEnd(ctx, p.ID)
	if err != nil {
		h.LogErr("web period: suggested end", err)
		return h.today(ctx)
	}
	return end
}

func (h *PeriodHandler) closeView(sc ProjectScope, p *period.Period, end civil.Date) templates.ClosePeriodView {
	ctx := sc.req.Context()
	v := templates.ClosePeriodView{
		OrgSlug:       sc.org.Slug,
		ProjectSlug:   sc.project.Slug,
		ProjectName:   sc.project.Name,
		PeriodID:      p.ID.String(),
		Start:         p.StartDate.String(),
		EndDate:       end.String(),
		MaxDate:       h.today(ctx).String(),
		DateLocked:    p.EndDate != nil,
		AlreadyClosed: p.IsLocked(),
		Label:         periodLabel(p, nil, nil),
	}
	if prev, next, err := h.Periods.Neighbors(ctx, p.ID); err == nil {
		v.Label = periodLabel(p, prev, next)
	} else {
		h.LogErr("web period: neighbors", err)
	}
	if p.IsLocked() {
		v.Preview, v.HasPreview = previewFromSnapshot(p.Snapshot)
		return v
	}
	snap, err := h.Periods.PreviewClose(ctx, p.ID, end)
	if ee, ok := errors.AsType[*period.EntriesAfterEndError](err); ok {
		v.EndError = endBeforeLatest(sc.req, ee, h.today(ctx))
		snap, err = h.Periods.PreviewClose(ctx, p.ID, ee.Latest)
	}
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

//i18n:use period.end_before_latest
//i18n:use period.end_future_entry
func endBeforeLatest(r *http.Request, ee *period.EntriesAfterEndError, today civil.Date) string {
	d := web.LayoutData{Translator: i18n.TranslatorFrom(r.Context())}
	if today.Before(ee.Latest) {
		return d.Tr("period.end_future_entry", "Latest", templates.LedgerDateText(d, ee.Latest))
	}
	return d.Tr("period.end_before_latest",
		"End", templates.LedgerDateText(d, ee.End), "Latest", templates.LedgerDateText(d, ee.Latest))
}

func (h *PeriodHandler) today(ctx context.Context) civil.Date {
	d, err := h.Periods.Today(ctx)
	if err != nil {
		h.LogErr("web period: today", err)
		return civil.DateOf(time.Now(), h.location(ctx))
	}
	return d
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

func applySnapshot(row *templates.PeriodRow, s *period.Snapshot) {
	if s == nil {
		return
	}
	row.HasTotals = true
	row.Income = money.New(s.Income, s.Currency)
	row.Expense = money.New(s.Expense, s.Currency)
	row.Net = money.New(s.Net, s.Currency)
	row.Adjust = money.New(s.Adjustments, s.Currency)
	row.Openings = money.New(s.Openings, s.Currency)
	row.TxCount = s.TxCount
}

func previewFromSnapshot(s *period.Snapshot) (templates.ClosePreview, bool) {
	if s == nil {
		return templates.ClosePreview{}, false
	}
	p := templates.ClosePreview{
		Income:   money.New(s.Income, s.Currency),
		Expense:  money.New(s.Expense, s.Currency),
		Net:      money.New(s.Net, s.Currency),
		Adjust:   money.New(s.Adjustments, s.Currency),
		Openings: money.New(s.Openings, s.Currency),
		TxCount:  s.TxCount,
		Wallets:  make([]templates.CloseWalletRow, 0, len(s.Wallets)),
	}
	for _, w := range s.Wallets {
		p.Wallets = append(p.Wallets, templates.CloseWalletRow{Name: w.Name, Closing: money.New(w.Closing, s.Currency)})
	}
	return p, true
}

// NOTE: the code is the stable handle on a refusal; the catalogue is docs/errors/README.md and docs/errors/yasaku.md.
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
		period.IsNotLatestClosedError(err) || period.IsEntriesAfterEndError(err)
}
