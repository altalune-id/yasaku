package handlers

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	slugs "altalune.id/yasaku/slug"
)

// OrgHandler owns /orgs, /orgs/new, /orgs/{slug} and members routes.
type OrgHandler struct{ Deps }

// NewOrgHandler wires the handler.
func NewOrgHandler(d Deps, orgs *org.Service) *OrgHandler {
	d.Orgs = orgs
	return &OrgHandler{Deps: d}
}

// GetList renders /orgs.
func (h *OrgHandler) GetList(w http.ResponseWriter, r *http.Request) {
	p, authed := h.requireAuth(w, r)
	if !authed {
		return
	}
	items, err := h.Orgs.List(r.Context(), p.UserID)
	if err != nil {
		h.LogErr("web org: list", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.list_failed"), "Could not load orgs.", err)
		return
	}
	blocked, _ := h.createBlocked(r, p.UserID)
	Render(w, r, templates.OrgsLayout(h.Layout(r, pageTitle(r, "orgs.title"), web.ActiveNav{Scope: web.NavScopeOrg}), templates.OrgsView{Orgs: orgSummaries(items), CreateBlocked: blocked}))
}

// GetNew renders /orgs/new.
func (h *OrgHandler) GetNew(w http.ResponseWriter, r *http.Request) {
	p, ok := h.requireAuth(w, r)
	if !ok {
		return
	}
	if !h.Caps.OrgCreation {
		h.orgCreationDisabled(w, r)
		return
	}
	v := templates.OrgNewView{Slug: slugs.Generate()}
	v.Error, v.ErrorCode = h.createBlocked(r, p.UserID)
	v.Blocked = v.Error != ""
	Render(w, r, templates.OrgNewLayout(h.Layout(r, pageTitle(r, "nav.create_organization"), web.ActiveNav{Scope: web.NavScopeOrg}), v))
}

// PostCreate handles POST /orgs (org creation is capability-gated).
func (h *OrgHandler) PostCreate(w http.ResponseWriter, r *http.Request) {
	p, sid, ok := h.LoadSession(r)
	if !ok {
		http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return
	}
	if !h.Caps.OrgCreation {
		h.orgCreationDisabled(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	slug := strings.TrimSpace(r.PostForm.Get("slug"))
	name := strings.TrimSpace(r.PostForm.Get("name"))
	created, err := h.Orgs.Create(r.Context(), org.CreateRequest{Slug: slug, Name: name, OwnerID: p.UserID})
	if err != nil {
		h.LogErr("web org: create", err)
		h.renderNewErr(w, r, slug, name, err)
		return
	}
	updated := p
	updated.ActiveOrgID = created.ID
	updated.ActiveProjectID = uuid.Nil
	if err := h.UpdateSession(r, sid, updated); err != nil {
		h.LogErr("web org: update session", err)
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/orgs/"+created.Slug+"/projects"), http.StatusSeeOther)
}

// PostRename handles POST /orgs/{slug}/rename.
func (h *OrgHandler) PostRename(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	o, r := sc.org, sc.req
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	// SECURITY: the slug comes from the URL, so membership in that org must be checked here — RLS no longer narrows this to the active org.
	canManage := holds(r, authz.MembersWrite)
	if !canManage {
		h.ErrorPage(w, r, http.StatusForbidden, pageTitle(r, "error_title.not_allowed"), "You must be an admin or owner to rename this organization.")
		return
	}
	if _, err := h.Orgs.Rename(r.Context(), o.ID, name); err != nil {
		h.LogErr("web org: rename", err)
		if org.IsSystemProtectedError(err) {
			h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.rename_not_allowed"), "This organization is system-protected.", err)
			return
		}
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.rename_failed"), err.Error())
		return
	}
	http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/orgs/"+o.Slug+"/members"), http.StatusSeeOther) //nolint:gosec // G710: destination sanitized via ResolveReturnTo → SanitizeReturnTo
}

// GetShow renders /orgs/{slug} — the members view.
func (h *OrgHandler) GetShow(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	p, o, r := sc.principal, sc.org, sc.req
	controls, grantable, err := h.Orgs.MemberControls(r.Context(), o.ID)
	if err != nil {
		h.LogErr("web org: members", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.members_failed"), "Could not load members.", err)
		return
	}
	roles := make([]string, 0, len(grantable))
	for _, g := range grantable {
		roles = append(roles, string(g))
	}
	Render(w, r, templates.MembersLayout(h.LayoutForOrg(r, pageTitle(r, "members.title", o.Name), o.Slug, "members"), templates.MembersView{
		OrgSlug:        o.Slug,
		OrgName:        o.Name,
		Members:        memberRows(controls, p.UserID),
		CanManage:      holds(r, authz.MembersWrite),
		GrantableRoles: roles,
	}))
}

// PostRemoveMember handles POST /orgs/{slug}/members/{user}/remove.
func (h *OrgHandler) PostRemoveMember(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	o, r := sc.org, sc.req
	canManage := holds(r, authz.MembersWrite)
	if !canManage {
		h.ErrorPage(w, r, http.StatusForbidden, pageTitle(r, "error_title.not_allowed"), "You must be an admin or owner to manage members.")
		return
	}
	userID, err := uuid.Parse(r.PathValue("user"))
	if err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_id"), "Malformed user id.")
		return
	}
	if err := h.Orgs.RemoveMember(r.Context(), o.ID, userID); err != nil {
		h.LogErr("web org: remove member", err)
		if org.IsSystemProtectedError(err) {
			h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.remove_not_allowed"), "This membership is system-protected.", err)
			return
		}
		if org.IsMembershipMissingError(err) || org.IsNotFoundError(err) {
			h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.member_not_found"), "That person is not a member of this organization.")
			return
		}
		if org.IsSelfRemovalError(err) {
			h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.remove_not_allowed"), "You cannot remove your own membership.")
			return
		}
		if org.IsOwnerRemovalError(err) {
			h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.remove_not_allowed"), "An owner cannot be removed.")
			return
		}
		if h.refuseRoleChange(w, r, err) {
			return
		}
		// SECURITY: err.Error() names internal ids, so it stays in the log and never reaches the page.
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.remove_failed"), "Could not remove that member.")
		return
	}
	http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/orgs/"+o.Slug+"/members"), http.StatusSeeOther) //nolint:gosec // G710: destination sanitized via ResolveReturnTo → SanitizeReturnTo
}

// PostSetRole handles POST /orgs/{slug}/members/{user}/role.
func (h *OrgHandler) PostSetRole(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	o, r := sc.org, sc.req
	userID, err := uuid.Parse(r.PathValue("user"))
	if err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_id"), "Malformed user id.")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	err = h.Orgs.SetRole(r.Context(), o.ID, userID, r.PostForm.Get("role"), uuid.Nil)
	switch {
	case err == nil:
		http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/orgs/"+o.Slug+"/members"), http.StatusSeeOther) //nolint:gosec // G710: destination sanitized via ResolveReturnTo → SanitizeReturnTo
	case authz.IsDeniedError(err):
		h.ErrorPage(w, r, http.StatusForbidden, pageTitle(r, "error_title.not_allowed"), "You must be an admin or owner to manage members.")
	case org.IsInvalidRoleError(err):
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_role"), "That role does not exist.")
	case org.IsMembershipMissingError(err), org.IsNotFoundError(err):
		h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.member_not_found"), "That person is not a member of this organization.")
	case org.IsSystemProtectedError(err):
		h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.not_allowed"), "This membership is system-protected.", err)
	case h.refuseRoleChange(w, r, err):
	default:
		h.LogErr("web org: set role", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.save_failed"), "Could not change that role.")
	}
}

func (h *OrgHandler) refuseRoleChange(w http.ResponseWriter, r *http.Request, err error) bool {
	ae, ok := apperror.AsAppError(err)
	switch {
	case !ok:
		return false
	case authz.IsEscalationError(err):
		h.ErrorPage(w, r, http.StatusForbidden, pageTitle(r, "error_title.not_allowed"), ae.Message(), err)
	case org.IsLastOwnerError(err):
		h.ErrorPage(w, r, http.StatusConflict, pageTitle(r, "error_title.not_allowed"), ae.Message(), err)
	default:
		return false
	}
	return true
}

func (h *OrgHandler) createBlocked(r *http.Request, userID uuid.UUID) (msg, code string) { //nolint:nonamedreturns // message and code differ in role
	if !h.Caps.OrgCreation {
		return "", ""
	}
	err := h.Orgs.CreateAllowed(r.Context(), userID)
	if err == nil {
		return "", ""
	}
	m, ok := h.limitMessage(r.Context(), err)
	if !ok {
		h.LogErr("web org: create allowed", err)
		return "", ""
	}
	return m, ErrorRef(err)
}

func (h *OrgHandler) requireAuth(w http.ResponseWriter, r *http.Request) (session.Principal, bool) {
	p, _, ok := h.LoadSession(r)
	if !ok {
		http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return session.Principal{}, false
	}
	return p, true
}

func (h *OrgHandler) renderNewErr(w http.ResponseWriter, r *http.Request, slug, name string, err error) {
	if slug == "" {
		slug = slugs.Generate()
	}
	msg := err.Error()
	switch {
	case org.IsAlreadyExistsError(err):
		msg = "Slug is already taken."
	case org.IsCreationDisabledError(err):
		msg = "Organization creation is disabled."
	}
	if m, ok := h.limitMessage(r.Context(), err); ok {
		msg = m
	}
	Render(w, r, templates.OrgNewLayout(
		h.Layout(r, pageTitle(r, "nav.create_organization"), web.ActiveNav{Scope: web.NavScopeOrg}),
		templates.OrgNewView{Slug: slug, Name: name, Error: msg, ErrorCode: ErrorRef(err)},
	))
}

func orgSummaries(items []*org.Org) []templates.OrgSummary {
	out := make([]templates.OrgSummary, 0, len(items))
	for _, o := range items {
		out = append(out, templates.OrgSummary{ID: o.ID.String(), Slug: o.Slug, Name: o.Name, System: o.System})
	}
	return out
}

func memberRows(controls []org.MemberControl, viewer uuid.UUID) []templates.MemberRow {
	out := make([]templates.MemberRow, 0, len(controls))
	for _, c := range controls {
		m := c.Member
		out = append(out, templates.MemberRow{
			UserID:       m.UserID.String(),
			Email:        m.Email,
			Name:         m.Name,
			Role:         string(m.Role),
			System:       m.System,
			IsSelf:       m.UserID == viewer,
			Removable:    c.Removable,
			RoleEditable: c.RoleEditable,
			CanMakeOwner: c.CanMakeOwner,
			LastOwner:    c.LastOwner,
		})
	}
	return out
}

// Register wires all org routes onto the mux.
func (h *OrgHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs", h.GetList)
	mux.HandleFunc("GET /orgs/new", h.GetNew)
	mux.HandleFunc("POST /orgs", h.PostCreate)
	mux.HandleFunc("GET /orgs/{org}/members", h.GetShow)
	mux.HandleFunc("POST /orgs/{org}/rename", h.PostRename)
	mux.HandleFunc("POST /orgs/{org}/members/{user}/remove", h.PostRemoveMember)
	mux.HandleFunc("POST /orgs/{org}/members/{user}/role", h.PostSetRole)
}

//i18n:use org_new.disabled_title
//i18n:use org_new.disabled

func (h *OrgHandler) orgCreationDisabled(w http.ResponseWriter, r *http.Request) {
	h.ErrorPage(w, r, http.StatusForbidden,
		localizedDefault(r, "org_new.disabled_title", "Not allowed"),
		localizedDefault(r, "org_new.disabled", "Organization creation is disabled in this deployment."))
}
