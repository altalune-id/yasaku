package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// InviteHandler owns /orgs/{slug}/invites and /invites/accept.
type InviteHandler struct {
	Deps
	Invites *invite.Service
}

// NewInviteHandler wires the handler.
func NewInviteHandler(d Deps, orgs *org.Service, invites *invite.Service) *InviteHandler {
	d.Orgs = orgs
	return &InviteHandler{Deps: d, Invites: invites}
}

// GetList renders /orgs/{slug}/invites.
func (h *InviteHandler) GetList(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	h.renderList(w, sc, "", "")
}

func (h *InviteHandler) renderList(w http.ResponseWriter, sc OrgScope, errMsg, errCode string) {
	p, o, r := sc.principal, sc.org, sc.req
	slug := o.Slug
	items, err := h.Invites.ListPending(r.Context())
	if err != nil {
		h.LogErr("web invite: list", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.list_failed"), "Could not load invites.", err)
		return
	}
	canManage, _ := h.Orgs.IsManager(r.Context(), o.ID, p.UserID)
	Render(w, r, templates.InvitesLayout(h.LayoutForOrg(r, pageTitle(r, "invites.title"), slug, "invites"), templates.InvitesView{
		OrgSlug: slug, Seats: h.seats(r), Invites: inviteRows(items), CanManage: canManage, Disabled: !h.Caps.InvitesEnabled,
		Error: errMsg, ErrorCode: errCode,
	}))
}

func (h *InviteHandler) seats(r *http.Request) *templates.SeatsView {
	if !h.Caps.InvitesEnabled {
		return nil
	}
	s, err := h.Invites.Seats(r.Context())
	if err != nil {
		h.LogErr("web invite: seats", err)
		return nil
	}
	if s.Unlimited() {
		return nil
	}
	return &templates.SeatsView{Used: s.Used, Max: s.Max, Pending: s.Pending}
}

// PostSend handles POST /orgs/{slug}/invites.
func (h *InviteHandler) PostSend(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	p, o, r := sc.principal, sc.org, sc.req
	slug := o.Slug
	canManage, _ := h.Orgs.IsManager(r.Context(), o.ID, p.UserID)
	if !canManage {
		h.ErrorPage(w, r, http.StatusForbidden, pageTitle(r, "error_title.not_allowed"), "Only owners and admins can invite.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	email := strings.TrimSpace(r.PostForm.Get("email"))
	roleStr := strings.TrimSpace(r.PostForm.Get("role"))
	role := invite.Role(roleStr)
	if !role.IsValid() {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_role"), "Role must be admin or member.")
		return
	}
	if _, err := h.Invites.Send(r.Context(), invite.SendRequest{Email: email, Role: role}); err != nil {
		h.LogErr("web invite: send", err)
		if invite.IsInvitesDisabledError(err) {
			h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.invites_disabled"), err.Error(), err)
			return
		}
		if m, ok := h.limitMessage(r.Context(), err); ok {
			h.renderList(w, sc, m, ErrorRef(err))
			return
		}
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.send_failed"), err.Error())
		return
	}
	http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/orgs/"+slug+"/invites"), http.StatusSeeOther) //nolint:gosec // G710: destination sanitized via ResolveReturnTo → SanitizeReturnTo
}

// PostRevoke handles POST /orgs/{slug}/invites/{id}/revoke.
func (h *InviteHandler) PostRevoke(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	p, o, r := sc.principal, sc.org, sc.req
	slug := o.Slug
	canManage, _ := h.Orgs.IsManager(r.Context(), o.ID, p.UserID)
	if !canManage {
		h.ErrorPage(w, r, http.StatusForbidden, pageTitle(r, "error_title.not_allowed"), "Only owners and admins can revoke invites.")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_id"), "Malformed invite id.")
		return
	}
	if err := h.Invites.Revoke(r.Context(), id); err != nil {
		h.LogErr("web invite: revoke", err)
		if invite.IsNotFoundError(err) {
			h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.invite_not_found"), "That invite no longer exists.", err)
			return
		}
		// SECURITY: err.Error() names internal ids, so it stays in the log and never reaches the page.
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.revoke_failed"), "Could not revoke that invite.")
		return
	}
	http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/orgs/"+slug+"/invites"), http.StatusSeeOther) //nolint:gosec // G710: destination sanitized via ResolveReturnTo → SanitizeReturnTo
}

// GetAccept handles GET /invites/accept?token=...
func (h *InviteHandler) GetAccept(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.missing_token"), "The invite link is malformed.")
		return
	}
	p, sid, authed := h.LoadSession(r)
	if !authed {
		web.SetCookie(w, web.CookieOpts{
			Name:         web.InviteCookieName,
			Value:        web.SignCookie(h.Key(web.PurposeInvite), token),
			BasePath:     h.Cfg.HTTP.BasePath,
			CookieSecure: h.SecureCookie(r),
			MaxAge:       int((15 * time.Minute).Seconds()),
		})
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return
	}
	res, err := h.Invites.Accept(r.Context(), invite.AcceptRequest{
		Token: token,
		Email: p.Email,
		Name:  p.Name,
	})
	if err != nil {
		switch {
		case invite.IsNotFoundError(err), invite.IsExpiredError(err), invite.IsAlreadyUsedError(err):
			h.ErrorPage(w, r, http.StatusGone, pageTitle(r, "error_title.invite_not_usable"), err.Error())
			return
		case limits.IsReachedError(err):
			msg, _ := h.limitMessage(r.Context(), err)
			h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.invite_not_usable"), msg, err)
			return
		case invite.IsTokenMismatchError(err):
			h.ErrorPage(w, r, http.StatusForbidden, pageTitle(r, "error_title.invite_mismatch"), "This invite is for a different email.")
			return
		default:
			h.LogErr("web invite: accept", err)
			// SECURITY: err.Error() names internal ids, so the page shows the code and request id instead.
			h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.accept_failed"), "Could not accept that invite.", err)
			return
		}
	}
	o, err := h.Orgs.ByID(r.Context(), res.Invite.OrgID)
	if err != nil {
		http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/orgs"), http.StatusSeeOther)
		return
	}
	web.ClearCookie(w, web.InviteCookieName, h.Cfg.HTTP.BasePath, h.SecureCookie(r))
	// Rehydrate the session with the just-joined org so the dashboard renders the new tenant immediately — the pre-invite Principal has ActiveOrgID = uuid.Nil.
	p.ActiveOrgID = o.ID
	if h.Projects != nil {
		tctx := tenant.Into(r.Context(), tenant.Context{OrgID: o.ID, UserID: p.UserID})
		if projects, plErr := h.Projects.List(tctx, o.ID); plErr == nil && len(projects) > 0 {
			p.ActiveProjectID = projects[0].ID
		}
	}
	if err := h.UpdateSession(r, sid, p); err != nil {
		h.LogErr("web invite: refresh session", err)
	}
	dest := "/orgs/" + o.Slug
	if h.Cfg.Compliance.RequireAcceptance && p.TermsAcceptedAt.IsZero() {
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/welcome")+"?return_to="+url.QueryEscape(dest), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, dest), http.StatusSeeOther) //nolint:gosec // G710: destination sanitized via ResolveReturnTo → SanitizeReturnTo
}

func inviteRows(items []*invite.Invite) []templates.InviteRow {
	out := make([]templates.InviteRow, 0, len(items))
	for _, i := range items {
		out = append(out, templates.InviteRow{
			ID:        i.ID.String(),
			Email:     i.Email,
			Role:      string(i.Role),
			ExpiresAt: i.ExpiresAt,
		})
	}
	return out
}

// Register wires all invite routes onto the mux.
func (h *InviteHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/invites", h.GetList)
	mux.HandleFunc("POST /orgs/{org}/invites", h.PostSend)
	mux.HandleFunc("POST /orgs/{org}/invites/{id}/revoke", h.PostRevoke)
	mux.HandleFunc("GET /invites/accept", h.GetAccept)
}
