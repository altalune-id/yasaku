package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

const settingsNavKey = "settings"

//nolint:gochecknoglobals // immutable table
var settingsTimezones = []string{
	"Asia/Jakarta",
	"Asia/Makassar",
	"Asia/Jayapura",
	"Asia/Singapore",
	"Asia/Kuala_Lumpur",
	"Asia/Tokyo",
	"UTC",
}

//nolint:gochecknoglobals // immutable table
var settingsCurrencies = []string{"IDR", "USD", "SGD", "MYR", "EUR", "JPY"}

//nolint:gochecknoglobals // immutable table
var settingsMCPScopes = []string{"yasaku:read", "yasaku:write"}

// SettingsHandler serves the per-project ledger settings screen.
type SettingsHandler struct {
	Deps
	Ledgers *ledger.Service
}

// NewSettingsHandler wires the handler.
func NewSettingsHandler(d Deps, projects *project.Service, ledgers *ledger.Service) *SettingsHandler {
	d.Projects = projects
	return &SettingsHandler{Deps: d, Ledgers: ledgers}
}

// Register wires the settings routes onto mux.
func (h *SettingsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/settings", h.GetSettings)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/settings", h.PostSettings)
}

// GetSettings renders the stored settings, falling back to the project defaults.
func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	st, err := h.Ledgers.Get(sc.req.Context())
	if err != nil {
		h.LogErr("web settings: get", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load project settings.", err)
		return
	}
	v := h.view(sc)
	v.Timezone, v.Currency, v.PeriodStartDay = st.Timezone, string(st.Currency), st.PeriodStartDay
	Render(w, sc.req, templates.SettingsLayout(h.layout(sc), v))
}

// PostSettings validates and persists the submitted settings, rendering a typed refusal as a banner.
func (h *SettingsHandler) PostSettings(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	tz := strings.TrimSpace(sc.req.PostForm.Get("timezone"))
	currency := strings.TrimSpace(sc.req.PostForm.Get("currency"))
	rawDay := strings.TrimSpace(sc.req.PostForm.Get("period_start_day"))

	v := h.view(sc)
	v.Timezone, v.Currency = tz, currency
	day, dayErr := strconv.Atoi(rawDay)
	if dayErr != nil {
		v.PeriodStartDay = h.storedStartDay(sc)
		v.Error = fmt.Sprintf("Period start day must be a whole number between %d and %d, got %q",
			ledger.MinPeriodStartDay, ledger.MaxPeriodStartDay, rawDay)
		v.ErrorCode = apperror.CodeLedgerInvalidStartDay
		Render(w, sc.req, templates.SettingsLayout(h.layout(sc), v))
		return
	}
	v.PeriodStartDay = day

	cur := money.Currency(currency)
	st, err := h.Ledgers.Update(sc.req.Context(), ledger.Patch{Timezone: &tz, Currency: &cur, PeriodStartDay: &day})
	if err != nil {
		if !isLedgerDomainError(err) {
			h.LogErr("web settings: update", err)
			h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Save failed", "Could not save project settings.", err)
			return
		}
		v.Error, v.ErrorCode = appErrorBanner(err)
		Render(w, sc.req, templates.SettingsLayout(h.layout(sc), v))
		return
	}
	v.Timezone, v.Currency, v.PeriodStartDay = st.Timezone, string(st.Currency), st.PeriodStartDay
	v.Saved = true
	Render(w, sc.req, templates.SettingsLayout(h.layout(sc), v))
}

func (h *SettingsHandler) requireProject(w http.ResponseWriter, r *http.Request) (projectScope, bool) {
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

func (h *SettingsHandler) storedStartDay(sc projectScope) int {
	st, err := h.Ledgers.Get(sc.req.Context())
	if err != nil {
		h.LogErr("web settings: stored start day", err)
		return ledger.DefaultPeriodStartDay
	}
	return st.PeriodStartDay
}

func (h *SettingsHandler) layout(sc projectScope) web.LayoutData {
	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, settingsNavKey)
	d.Title = d.Tr("settings.title") + " · " + sc.project.Name
	return d
}

func (h *SettingsHandler) view(sc projectScope) templates.SettingsView {
	v := templates.SettingsView{
		OrgSlug:     sc.org.Slug,
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Timezones:   settingsTimezones,
		Currencies:  settingsCurrencies,
	}
	if h.Caps.MCPEnabled {
		v.MCPEndpoint = h.Cfg.MCP.Audience
		v.MCPScopes = settingsMCPScopes
	}
	return v
}

func isLedgerDomainError(err error) bool {
	return ledger.IsInvalidTimezoneError(err) || ledger.IsInvalidStartDayError(err) ||
		ledger.IsUnknownCurrencyError(err) || ledger.IsNotFoundError(err)
}
