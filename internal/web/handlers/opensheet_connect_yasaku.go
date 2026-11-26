package handlers

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/altalune-id/oidcrp"
	"github.com/google/uuid"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

const opensheetStateCookie = "yasaku_opensheet_state"

// OpensheetCallbackPath is the redirect URI path of the OAuth connect to opensheet, under the base path.
const OpensheetCallbackPath = "/opensheet/oauth/callback"

const (
	connectFailed    = "failed"
	connectCancelled = "cancelled"
	connectNoRefresh = "no_refresh"
)

// OpensheetAuth is the OAuth client that asks the upstream IdP for an opensheet grant; *oidcrp.Client is one.
type OpensheetAuth interface {
	AuthCodeURL(ctx context.Context, req oidcrp.AuthRequest) (string, error)
	ExchangeTokens(ctx context.Context, cb oidcrp.Callback, req oidcrp.AuthRequest) (*oidcrp.TokenSet, error)
}

// SECURITY: the state cookie binds one authorization response to the org, project and person that started it.
type opensheetState struct {
	State       string    `json:"s"`
	Nonce       string    `json:"n"`
	Verifier    string    `json:"v"`
	OrgSlug     string    `json:"os"`
	ProjectSlug string    `json:"ps"`
	OrgID       uuid.UUID `json:"o"`
	ProjectID   uuid.UUID `json:"p"`
	UserID      uuid.UUID `json:"u"`
	Expires     int64     `json:"e"`
}

// PostConnect sends a manager to the upstream IdP to grant yasaku access to opensheet; htmx gets HX-Redirect so the button can show it is busy.
func (h *OpensheetHandler) PostConnect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if h.Auth == nil || len(h.Secret) == 0 {
		h.ErrorPage(w, sc.req, http.StatusNotFound, pageTitle(sc.req, "error_title.not_found"), "")
		return
	}
	if err := h.Opensheet.Authorize(sc.req.Context()); err != nil {
		h.refuse(w, sc, nil, nil, err, templates.OpensheetModule)
		return
	}
	req, err := oidcrp.NewAuthRequest("", "")
	if err != nil {
		h.connectUnavailable(w, sc, err)
		return
	}
	target, err := h.Auth.AuthCodeURL(sc.req.Context(), req)
	if err != nil {
		h.connectUnavailable(w, sc, err)
		return
	}
	raw, err := json.Marshal(opensheetState{
		State: req.State, Nonce: req.Nonce, Verifier: req.CodeVerifier,
		OrgSlug: sc.org.Slug, ProjectSlug: sc.project.Slug, OrgID: sc.org.ID, ProjectID: sc.project.ID, UserID: sc.principal.UserID,
		Expires: time.Now().Add(oidcStateTTL).Unix(),
	})
	if err != nil {
		h.connectUnavailable(w, sc, err)
		return
	}
	web.SetCookie(w, web.CookieOpts{
		Name: opensheetStateCookie, Value: web.SignCookie(h.Key(web.PurposeOpensheetState), base64.RawURLEncoding.EncodeToString(raw)),
		BasePath: h.Cfg.HTTP.BasePath, CookieSecure: h.SecureCookie(r), MaxAge: int(oidcStateTTL.Seconds()),
	})
	if web.IsHTMXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusFound) //nolint:gosec // G710: the authorization endpoint of the issuer's own discovery.
}

func (h *OpensheetHandler) connectUnavailable(w http.ResponseWriter, sc ProjectScope, err error) {
	h.LogErr("web opensheet: connect", err)
	if web.IsHTMXRequest(sc.req) {
		d := h.opensheetLayout(sc)
		RenderStatus(w, sc.req, http.StatusBadGateway, templates.ErrorBanner(d, d.Tr("opensheet.connect.failed"), ""))
		return
	}
	h.ErrorPage(w, sc.req, http.StatusBadGateway, pageTitle(sc.req, "error_title.connect_failed"), localizedDefault(sc.req, "opensheet.connect.failed", "Connecting to OpenSheet failed."), err)
}

//i18n:use opensheet.connect.expired

// GetCallback checks the authorization response against the state cookie and the person and project it was started for, then saves the grant. SECURITY: the callback URL carries the code, so it sends no Referer.
func (h *OpensheetHandler) GetCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	st, ok := h.readState(r)
	web.ClearCookie(w, opensheetStateCookie, h.Cfg.HTTP.BasePath, h.SecureCookie(r))
	if !ok || h.Auth == nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.connect_failed"), localizedDefault(r, "opensheet.connect.expired", "This connect link expired. Connect again from the OpenSheet page."))
		return
	}
	r.SetPathValue("org", st.OrgSlug)
	r.SetPathValue("project", st.ProjectSlug)
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if sc.org.ID != st.OrgID || sc.project.ID != st.ProjectID || sc.principal.UserID != st.UserID {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.connect_failed"), localizedDefault(sc.req, "opensheet.connect.expired", "This connect link expired. Connect again from the OpenSheet page."))
		return
	}
	// SECURITY: checked before the code is redeemed, so a person who lost ledger:settings mid-flow never mints a refresh token.
	if err := h.Opensheet.Authorize(sc.req.Context()); err != nil {
		h.refuse(w, sc, nil, nil, err, templates.OpensheetModule)
		return
	}
	page := web.Path(h.Cfg.HTTP.BasePath, "/orgs/"+sc.org.Slug+"/projects/"+sc.project.Slug+"/opensheet")
	req := oidcrp.AuthRequest{State: st.State, Nonce: st.Nonce, CodeVerifier: st.Verifier}
	set, err := h.Auth.ExchangeTokens(sc.req.Context(), oidcrp.ParseCallback(r.URL.Query()), req)
	if err != nil {
		h.LogErr("web opensheet: callback", err)
		h.connectOutcome(w, r, page, connectOutcomeOf(err))
		return
	}
	if _, err := h.Opensheet.Connect(sc.req.Context(), *set); err != nil {
		if opensheetsync.IsConnectFailedError(err) {
			h.connectOutcome(w, r, page, connectNoRefresh)
			return
		}
		h.refuse(w, sc, nil, nil, err, templates.OpensheetModule)
		return
	}
	http.Redirect(w, r, page, http.StatusSeeOther)
}

func connectOutcomeOf(err error) string {
	var rpErr *oidcrp.Error
	if errors.As(err, &rpErr) && rpErr.Reason == oidcrp.ReasonCancelled {
		return connectCancelled
	}
	return connectFailed
}

func (h *OpensheetHandler) connectOutcome(w http.ResponseWriter, r *http.Request, page, outcome string) {
	http.Redirect(w, r, page+"?connect="+url.QueryEscape(outcome), http.StatusSeeOther)
}

func (h *OpensheetHandler) readState(r *http.Request) (opensheetState, bool) {
	c, err := r.Cookie(opensheetStateCookie)
	if err != nil || len(h.Secret) == 0 {
		return opensheetState{}, false
	}
	value, err := web.VerifyCookie(h.Key(web.PurposeOpensheetState), c.Value)
	if err != nil {
		return opensheetState{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return opensheetState{}, false
	}
	var st opensheetState
	if json.Unmarshal(raw, &st) != nil || st.State == "" || time.Now().Unix() >= st.Expires {
		return opensheetState{}, false
	}
	return st, true
}

// PostProvision creates the Sheet in the picked opensheet project and links it.
func (h *OpensheetHandler) PostProvision(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	if err := sc.req.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, pageTitle(sc.req, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	pick := opensheetsync.Pick{
		OSProjectID:  strings.TrimSpace(sc.req.PostForm.Get("os_project_id")),
		CredentialID: strings.TrimSpace(sc.req.PostForm.Get("credential_id")),
		ProjectName:  sc.project.Name,
	}
	if _, err := h.Opensheet.Provision(sc.req.Context(), pick); err != nil {
		h.refuse(w, sc, nil, nil, err, templates.OpensheetModule)
		return
	}
	h.module(w, sc, "")
}

//i18n:use opensheet.connect.failed
//i18n:use opensheet.connect.cancelled
//i18n:use opensheet.connect.no_refresh
func connectBanner(outcome string) banner {
	switch outcome {
	case connectCancelled:
		return banner{Key: "opensheet.connect.cancelled"}
	case connectNoRefresh:
		return banner{Key: "opensheet.connect.no_refresh", Code: apperror.CodeOpensheetConnectFailed}
	case connectFailed:
		return banner{Key: "opensheet.connect.failed"}
	}
	return banner{}
}

func (h *OpensheetHandler) connectView(sc ProjectScope, v *templates.OpensheetView, st opensheetsync.Status) {
	access := st.Access
	if st.Link != nil && st.Link.Credential != opensheetsync.CredentialGrant {
		return
	}
	v.CanConnect = h.Auth != nil
	v.OpensheetURL = h.Cfg.Opensheet.BaseURL
	v.Access = string(access)
	cb := connectBanner(sc.req.URL.Query().Get("connect"))
	v.ConnectKey, v.ConnectCode = cb.Key, cb.Code
	if access != opensheetsync.AccessPending || !holds(sc.req, authz.LedgerSettings) {
		return
	}
	choices, err := h.Opensheet.Choices(sc.req.Context())
	if err != nil {
		h.LogErr("web opensheet: choices", err)
		b := opensheetBannerFrom(err)
		v.ErrorKey, v.ErrorMsg, v.ErrorCode = b.Key, b.Msg, b.Code
		return
	}
	v.SetupNeeded = !choices.Ready()
	for _, p := range choices.Projects {
		v.PickProjects = append(v.PickProjects, templates.OpensheetChoice{ID: p.ID, Label: p.Name + " (" + p.OrgSlug + "/" + p.Slug + ")"})
	}
	names := map[string]string{}
	for _, p := range choices.Projects {
		names[p.ID] = p.Name
	}
	for _, c := range choices.Credentials {
		label := cmp.Or(c.GoogleAccountEmail, c.Name)
		if len(choices.Projects) > 1 {
			label += " (" + names[c.ProjectID] + ")"
		}
		v.PickCredentials = append(v.PickCredentials, templates.OpensheetChoice{ID: c.ID, Label: label})
	}
}
