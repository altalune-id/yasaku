package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	"altalune.id/yasaku/money"
)

const (
	settingsNavKey       = "settings"
	settingsLedgerMember = "Only an owner or admin can change these settings."
)

//i18n:use settings.name_error
//i18n:use settings.name_locked

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

// SettingsHandler serves the per-project ledger settings screen.
type SettingsHandler struct {
	Deps
	Ledgers   *ledger.Service
	MCPScopes []string
}

// NewSettingsHandler wires the handler; mcpScopes are the scopes the MCP surface publishes.
func NewSettingsHandler(d Deps, projects *project.Service, ledgers *ledger.Service, mcpScopes []string) *SettingsHandler {
	d.Projects = projects
	return &SettingsHandler{Deps: d, Ledgers: ledgers, MCPScopes: mcpScopes}
}

// Register wires the settings routes onto mux.
func (h *SettingsHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/settings", h.GetSettings)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/settings", h.PostSettings)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/rename", h.PostRename)
}

// GetSettings renders the stored settings, falling back to the project defaults.
func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	st, err := h.Ledgers.Get(sc.req.Context())
	if err != nil {
		h.LogErr("web settings: get", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.load_failed"), "Could not load project settings.", err)
		return
	}
	v := h.view(sc, holds(sc.req, authz.LedgerSettings))
	v.Timezone, v.Currency, v.PeriodStartDay = st.Timezone, string(st.Currency), st.PeriodStartDay
	v.NameSaved = sc.req.URL.Query().Get("saved") == "name"
	Render(w, sc.req, templates.SettingsLayout(h.layout(sc), v))
}

//i18n:use settings.name_forbidden_title
//i18n:use settings.ledger_member

// PostRename renames the project for a manager and returns to settings, re-rendering it with a field error on refusal.
func (h *SettingsHandler) PostRename(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	r = sc.req
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if _, err := h.Projects.Rename(r.Context(), sc.project.ID, name); err != nil {
		if h.refuseNotManager(w, r, err, "settings.name_member", "Only an owner or admin can rename this project.") {
			return
		}
		status, key := http.StatusUnprocessableEntity, "settings.name_error"
		switch {
		case project.IsSystemProtectedError(err):
			status, key = http.StatusConflict, "settings.name_locked"
		case !project.IsInvalidNameError(err):
			h.LogErr("web settings: rename", err)
			h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.rename_failed"), "Could not rename the project.", err)
			return
		}
		v := h.view(sc, true)
		st, gErr := h.Ledgers.Get(r.Context())
		if gErr != nil {
			h.LogErr("web settings: get", gErr)
			h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.load_failed"), "Could not load project settings.", gErr)
			return
		}
		v.Timezone, v.Currency, v.PeriodStartDay = st.Timezone, string(st.Currency), st.PeriodStartDay
		v.Name, v.NameError = name, key
		RenderStatus(w, r, status, templates.SettingsLayout(h.layout(sc), v))
		return
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, "/settings?saved=name")), http.StatusSeeOther) //nolint:gosec // G710: both slugs come from the store and passed their slug patterns
}

// PostSettings validates and persists the submitted settings, rendering a typed refusal as a banner.
func (h *SettingsHandler) PostSettings(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if err := authz.Can(sc.req.Context(), authz.LedgerSettings); err != nil {
		if !h.refuseNotManager(w, sc.req, err, "settings.ledger_member", settingsLedgerMember) {
			h.LogErr("web settings: role", err)
			h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.save_failed"), "Could not save project settings.", err)
		}
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	tz := strings.TrimSpace(sc.req.PostForm.Get("timezone"))
	currency := strings.TrimSpace(sc.req.PostForm.Get("currency"))
	rawDay := strings.TrimSpace(sc.req.PostForm.Get("period_start_day"))

	v := h.view(sc, true)
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
		if h.refuseNotManager(w, sc.req, err, "settings.ledger_member", settingsLedgerMember) {
			return
		}
		if !isLedgerDomainError(err) {
			h.LogErr("web settings: update", err)
			h.ErrorPage(w, sc.req, http.StatusInternalServerError, pageTitle(sc.req, "error_title.save_failed"), "Could not save project settings.", err)
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

func (h *SettingsHandler) storedStartDay(sc ProjectScope) int {
	st, err := h.Ledgers.Get(sc.req.Context())
	if err != nil {
		h.LogErr("web settings: stored start day", err)
		return ledger.DefaultPeriodStartDay
	}
	return st.PeriodStartDay
}

func (h *SettingsHandler) layout(sc ProjectScope) web.LayoutData {
	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, settingsNavKey)
	d.Title = pageTitle(sc.req, "settings.title", sc.project.Name)
	return d
}

func (h *SettingsHandler) view(sc ProjectScope, canManage bool) templates.SettingsView {
	v := templates.SettingsView{
		OrgSlug:     sc.org.Slug,
		OrgID:       sc.org.ID.String(),
		ProjectSlug: sc.project.Slug,
		ProjectID:   sc.project.ID.String(),
		ProjectName: sc.project.Name,
		Name:        sc.project.Name,
		System:      sc.project.System,
		CanRename:   !sc.project.System && canManage,
		CanManage:   canManage,
		Timezones:   settingsTimezones,
		Currencies:  settingsCurrencies,
		StartDays:   settingsStartDays(),
	}
	if h.Caps.MCPEnabled {
		v.MCPEndpoint = h.Cfg.MCP.Audience
		v.MCPScopes = h.MCPScopes
	}
	return v
}

func settingsStartDays() []int {
	days := make([]int, 0, ledger.MaxPeriodStartDay)
	for d := ledger.MinPeriodStartDay; d <= ledger.MaxPeriodStartDay; d++ {
		days = append(days, d)
	}
	return days
}

func isLedgerDomainError(err error) bool {
	return ledger.IsInvalidTimezoneError(err) || ledger.IsInvalidStartDayError(err) ||
		ledger.IsUnknownCurrencyError(err) || ledger.IsNotFoundError(err)
}
