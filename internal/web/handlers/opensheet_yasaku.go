package handlers

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// OpensheetHandler owns the project-scoped Opensheet module page; boot mounts it only when opensheet.baseURL is set.
type OpensheetHandler struct {
	Deps
	Opensheet *opensheetsync.Service
}

// NewOpensheetHandler wires the handler; d carries Orgs and Projects.
func NewOpensheetHandler(d Deps, svc *opensheetsync.Service) *OpensheetHandler {
	return &OpensheetHandler{Deps: d, Opensheet: svc}
}

// Register wires the Opensheet routes onto mux.
func (h *OpensheetHandler) Register(mux web.Mux) {
	const base = "/orgs/{org}/projects/{project}/opensheet"
	mux.HandleFunc("GET "+base, h.GetPage)
	mux.HandleFunc("POST "+base, h.PostSave)
	mux.HandleFunc("POST "+base+"/test", h.PostTest)
	mux.HandleFunc("POST "+base+"/enabled", h.PostEnabled)
	mux.HandleFunc("POST "+base+"/sync", h.PostSync)
	mux.HandleFunc("POST "+base+"/delete", h.PostDelete)
}

// GetPage renders the module page: status, tutorial and form.
func (h *OpensheetHandler) GetPage(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	v, err := h.view(sc, nil)
	if err != nil {
		h.LogErr("web opensheet: status", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load the Opensheet link.", err)
		return
	}
	Render(w, sc.req, templates.OpensheetLayout(h.opensheetLayout(sc), v))
}

// PostTest checks the typed settings against opensheet and returns the checklist.
func (h *OpensheetHandler) PostTest(w http.ResponseWriter, r *http.Request) {
	sc, in, form, ok := h.requireSettings(w, r)
	if !ok {
		return
	}
	cl, err := h.Opensheet.Test(sc.req.Context(), in)
	if err != nil {
		h.refuse(w, sc, &form, nil, err, templates.OpensheetChecks)
		return
	}
	v, vErr := h.view(sc, &form)
	if vErr != nil {
		h.refuse(w, sc, &form, nil, vErr, templates.OpensheetChecks)
		return
	}
	v.Checks = checksOf(cl)
	h.respond(w, sc, http.StatusOK, v, templates.OpensheetChecks)
}

// PostSave re-runs the Test on the server and saves only when every tab passes; a refusal swaps only the checklist.
func (h *OpensheetHandler) PostSave(w http.ResponseWriter, r *http.Request) {
	sc, in, form, ok := h.requireSettings(w, r)
	if !ok {
		return
	}
	_, cl, err := h.Opensheet.Save(sc.req.Context(), in)
	if err != nil {
		h.refuse(w, sc, &form, cl, err, templates.OpensheetChecks)
		return
	}
	h.module(w, sc, "opensheet.saved")
}

// PostEnabled turns the mirror on or off.
func (h *OpensheetHandler) PostEnabled(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	on, _ := strconv.ParseBool(sc.req.PostForm.Get("enabled"))
	if _, err := h.Opensheet.SetEnabled(sc.req.Context(), on); err != nil {
		h.refuse(w, sc, nil, nil, err, templates.OpensheetStatus)
		return
	}
	h.module(w, sc, "")
}

// PostSync marks every row of the project for the mirror.
func (h *OpensheetHandler) PostSync(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if _, err := h.Opensheet.SyncNow(sc.req.Context()); err != nil {
		h.refuse(w, sc, nil, nil, err, templates.OpensheetStatus)
		return
	}
	h.module(w, sc, h.syncNotice())
}

// PostDelete removes the link and its sync state; the sheet is left alone.
func (h *OpensheetHandler) PostDelete(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if err := h.Opensheet.Delete(sc.req.Context()); err != nil {
		h.refuse(w, sc, nil, nil, err, templates.OpensheetStatus)
		return
	}
	h.module(w, sc, "")
}

func (h *OpensheetHandler) requireSettings(w http.ResponseWriter, r *http.Request) (ProjectScope, opensheetsync.Settings, templates.OpensheetForm, bool) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return ProjectScope{}, opensheetsync.Settings{}, templates.OpensheetForm{}, false
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return ProjectScope{}, opensheetsync.Settings{}, templates.OpensheetForm{}, false
	}
	f := sc.req.PostForm
	form := templates.OpensheetForm{
		OSOrg:             strings.TrimSpace(f.Get("os_org")),
		OSProject:         strings.TrimSpace(f.Get("os_project")),
		TransactionsSheet: strings.TrimSpace(f.Get("transactions_sheet")),
		WalletsSheet:      strings.TrimSpace(f.Get("wallets_sheet")),
		CategoriesSheet:   strings.TrimSpace(f.Get("categories_sheet")),
	}
	in := opensheetsync.Settings{
		OSOrg: form.OSOrg, OSProject: form.OSProject, APIKey: f.Get("api_key"),
		Sheets: opensheetsync.SheetSlugs{Transactions: form.TransactionsSheet, Wallets: form.WalletsSheet, Categories: form.CategoriesSheet},
	}
	return sc, in, form, true
}

const syncDoneNotice = "opensheet.sync_done"

//i18n:use opensheet.sync_queued
//i18n:use opensheet.sync_done
func (h *OpensheetHandler) syncNotice() string {
	if h.Cfg.Queue.Enabled {
		return "opensheet.sync_queued"
	}
	return syncDoneNotice
}

//i18n:use opensheet.saved
func (h *OpensheetHandler) module(w http.ResponseWriter, sc ProjectScope, notice string) {
	v, err := h.view(sc, nil)
	if err != nil {
		h.LogErr("web opensheet: status", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load the Opensheet link.", err)
		return
	}
	v.NoticeKey = notice
	if notice == syncDoneNotice && v.LastError != "" {
		v.NoticeKey = ""
	}
	h.respond(w, sc, http.StatusOK, v, templates.OpensheetModule)
}

// NOTE: a 422 swaps only the named fragment, so the form the person typed into, the API key included, stays in the browser and is never echoed back.
func (h *OpensheetHandler) refuse(w http.ResponseWriter, sc ProjectScope, form *templates.OpensheetForm, cl opensheetsync.Checklist, err error, fragment func(web.LayoutData, templates.OpensheetView) templ.Component) {
	status := opensheetStatus(err)
	if status >= http.StatusInternalServerError {
		h.LogErr("web opensheet", err)
	}
	v, vErr := h.view(sc, form)
	if vErr != nil {
		h.LogErr("web opensheet: status", vErr)
	}
	b := opensheetBannerFrom(err)
	v.ErrorKey, v.ErrorMsg, v.ErrorCode = b.Key, b.Msg, b.Code
	v.Checks = checksOf(cl)
	if status != http.StatusUnprocessableEntity && web.IsHTMXRequest(sc.req) {
		d := h.opensheetLayout(sc)
		msg := b.Msg
		if b.Key != "" {
			msg = d.Tr(b.Key)
		}
		RenderStatus(w, sc.req, status, templates.ErrorBanner(d, msg, b.Code))
		return
	}
	h.respond(w, sc, status, v, fragment)
}

func (h *OpensheetHandler) respond(w http.ResponseWriter, sc ProjectScope, status int, v templates.OpensheetView, fragment func(web.LayoutData, templates.OpensheetView) templ.Component) {
	d := h.opensheetLayout(sc)
	if !web.IsHTMXRequest(sc.req) {
		RenderStatus(w, sc.req, status, templates.OpensheetLayout(d, v))
		return
	}
	RenderStatus(w, sc.req, status, fragment(d, v))
}

func (h *OpensheetHandler) opensheetLayout(sc ProjectScope) web.LayoutData {
	return h.LayoutForProject(sc.req, "Opensheet · "+sc.project.Name, sc.org.Slug, sc.project, "opensheet")
}

//i18n:use opensheet.tab.*
//i18n:use opensheet.check.*
func (h *OpensheetHandler) view(sc ProjectScope, form *templates.OpensheetForm) (templates.OpensheetView, error) {
	v := templates.OpensheetView{ProjectSlug: sc.project.Slug, CanManage: h.canManage(sc)}
	for _, tab := range opensheetsync.Contract() {
		v.Tabs = append(v.Tabs, templates.OpensheetTab{
			LabelKey: "opensheet.tab." + string(tab.Entity), Slug: tab.DefaultSlug, Columns: tab.Columns, HeaderTSV: tab.HeaderTSV(),
		})
	}
	v.Form = formOf(opensheetsync.Settings{Sheets: opensheetsync.DefaultSheetSlugs()})
	st, err := h.Opensheet.Status(sc.req.Context())
	if err != nil {
		return v, err
	}
	v.Pending, v.Failing, v.GivenUp = st.Pending, st.Failing, st.GivenUp
	if l := st.Link; l != nil {
		v.Linked, v.Enabled, v.Verified, v.AutoDisabled = true, l.Enabled, l.VerifiedAt != nil, l.AutoDisabled()
		v.AwaitingFirstSync = l.AwaitingFirstSync()
		v.LastSyncedAt, v.KeyHint = l.LastSyncedAt, l.APIKeyHint
		// NOTE: a passing Save resets the streak and keeps last_error, so a zero streak means the error predates the current settings.
		if l.FailureStreak > 0 {
			v.LastError = l.LastError
		}
		v.Form = formOf(l.Settings())
	}
	if form != nil {
		v.Form = *form
	}
	return v, nil
}

func (h *OpensheetHandler) canManage(sc ProjectScope) bool {
	ok, err := h.Orgs.IsManager(sc.req.Context(), sc.org.ID, sc.principal.UserID)
	if err != nil {
		h.LogErr("web opensheet: role", err)
	}
	return ok
}

func formOf(s opensheetsync.Settings) templates.OpensheetForm {
	return templates.OpensheetForm{
		OSOrg: s.OSOrg, OSProject: s.OSProject,
		TransactionsSheet: s.Sheets.Transactions, WalletsSheet: s.Sheets.Wallets, CategoriesSheet: s.Sheets.Categories,
	}
}

func checksOf(cl opensheetsync.Checklist) []templates.OpensheetCheck {
	out := make([]templates.OpensheetCheck, 0, len(cl))
	for _, c := range cl {
		out = append(out, templates.OpensheetCheck{
			LabelKey: "opensheet.tab." + string(c.Entity), Sheet: c.Sheet, OK: c.OK(),
			Reachable: c.Reachable, Unavailable: opensheetsync.IsUnavailableError(c.Err), PrivateEndpoint: opensheetsync.IsPrivateEndpointError(c.Err),
			IDColumn: c.IDColumn, Writable: c.Writable, Missing: strings.Join(c.Missing, ", "),
			MissingDeletedAt: slices.Contains(c.Missing, "deleted_at"), ColumnsDeferred: c.ColumnsDeferred,
			ContractReason: c.ContractReason, Code: ErrorRef(c.Err),
		})
	}
	return out
}

//i18n:use opensheet.error.key_unreadable
func opensheetBannerFrom(err error) banner {
	if opensheetsync.IsKeyUnreadableError(err) {
		return banner{Key: "opensheet.error.key_unreadable", Code: ErrorRef(err)}
	}
	return bannerFrom(err)
}

func opensheetStatus(err error) int {
	ae, ok := apperror.AsAppError(err)
	if !ok {
		return http.StatusInternalServerError
	}
	// NOTE: an opensheet that did not answer is a failed check the person can retry, so it lands in the checklist like any other.
	switch ae.GRPCCode() {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.Unavailable:
		return http.StatusUnprocessableEntity
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}
