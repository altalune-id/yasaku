package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// OrgScope is the org an org-scoped route acts on, with a request already carrying the tenant scope.
type OrgScope struct {
	principal session.Principal
	sid       string
	org       *org.Org
	req       *http.Request
}

// ProjectScope is the org and project a project-scoped route acts on, with a request already carrying both scopes.
type ProjectScope struct {
	principal session.Principal
	sid       string
	org       *org.Org
	project   *project.Project
	req       *http.Request
}

// RequireOrg resolves the org the path names, gating membership before anything reads its rows. SECURITY: the slug is attacker-supplied and RLS cannot gate it, so this membership check is the only thing separating one org's members from another's rows.
func (d Deps) RequireOrg(w http.ResponseWriter, r *http.Request) (OrgScope, bool) {
	return d.RequireOrgFrom(w, r, func(r *http.Request) string { return r.PathValue("org") })
}

// RequireOrgFrom is RequireOrg for a route that names its org elsewhere, such as a form field; slug is read only once the caller is signed in.
func (d Deps) RequireOrgFrom(w http.ResponseWriter, r *http.Request, slug func(*http.Request) string) (OrgScope, bool) {
	p, sid, ok := d.RequireSignedIn(w, r)
	if !ok {
		return OrgScope{}, false
	}
	o, r, ok := d.OrgScopeFor(w, r, p, slug(r))
	if !ok {
		return OrgScope{}, false
	}
	return OrgScope{principal: p, sid: sid, org: o, req: r}, true
}

// RequireSignedIn loads the caller's session, redirecting to login when there is none.
func (d Deps) RequireSignedIn(w http.ResponseWriter, r *http.Request) (session.Principal, string, bool) {
	p, sid, ok := d.loadSession(r)
	if !ok || p.UserID == uuid.Nil {
		http.Redirect(w, r, ResolveReturnTo(d.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return session.Principal{}, "", false
	}
	if d.Users != nil {
		if err := d.Users.RequireActive(r.Context(), p.UserID); err != nil {
			d.refuseInactive(w, r, err)
			return session.Principal{}, "", false
		}
	}
	return p, sid, true
}

//i18n:use user.suspended_title

// SECURITY: a suspended user keeps the session, so an unsuspend restores it, but reaches no page that needs one.
func (d Deps) refuseInactive(w http.ResponseWriter, r *http.Request, err error) {
	if user.IsSuspendedError(err) {
		title, msg := pageTitle(r, "user.suspended_title"), suspendedMessage(r, d.Cfg.Compliance.ContactEmail)
		if web.IsHTMXRequest(r) {
			d.ErrorPage(w, r, http.StatusForbidden, title, msg, err)
			return
		}
		RenderStatus(w, r, http.StatusForbidden, templates.SuspendedLayout(d.Base(r, title), msg))
		return
	}
	http.Redirect(w, r, ResolveReturnTo(d.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
}

//i18n:use user.suspended_message

func suspendedMessage(r *http.Request, contact string) string {
	if contact == "" {
		return ""
	}
	return i18n.TranslatorFrom(r.Context()).T("user.suspended_message", "Contact", contact)
}

// RequireStaff loads the caller's session and admits platform staff only. SECURITY: anyone else gets a 404, so /admin reveals nothing to non-staff.
func (d Deps) RequireStaff(w http.ResponseWriter, r *http.Request) (session.Principal, string, bool) {
	p, sid, ok := d.RequireSignedIn(w, r)
	if !ok {
		return session.Principal{}, "", false
	}
	staff, err := d.Orgs.IsStaff(r.Context(), p.UserID)
	if err != nil || !staff {
		d.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.not_found"), "", err)
		return session.Principal{}, "", false
	}
	return p, sid, true
}

//i18n:use authz.support_read_only

// SECURITY: a non-member enters an org only on a live support visit to that org, only while still staff, and only to read.
func (d Deps) admitSupport(w http.ResponseWriter, r *http.Request, p session.Principal, o *org.Org, cause error) bool {
	if p.Support == nil || p.Support.OrgID != o.ID {
		d.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.organization_not_found"), "", cause)
		return false
	}
	if !p.Support.Live(o.ID, time.Now()) {
		d.endSupport(w, r)
		return false
	}
	staff, err := d.Orgs.IsStaff(r.Context(), p.UserID)
	if err != nil || !staff {
		d.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.organization_not_found"), "", cause)
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		d.ErrorPage(w, r, http.StatusForbidden, pageTitle(r, "error_title.not_allowed"),
			i18n.TranslatorFrom(r.Context()).T("authz.support_read_only", "Org", o.Name), &authz.SupportReadOnlyError{Org: o.Name})
		return false
	}
	return true
}

func (d Deps) endSupport(w http.ResponseWriter, r *http.Request) {
	if p, sid, ok := d.LoadSession(r); ok {
		p.Support = nil
		if err := d.UpdateSession(r, sid, p); err != nil {
			d.LogErr("support: clear expired visit", err)
		}
	}
	http.Redirect(w, r, ResolveReturnTo(d.Cfg.HTTP.BasePath, "/admin"), http.StatusSeeOther)
}

// MemberOrgScopes returns an OrgScope for every org the caller belongs to. SECURITY: org.Service.List joins the caller's memberships, so every org here is one the caller is a member of right now.
func (d Deps) MemberOrgScopes(r *http.Request, p session.Principal, sid string) ([]OrgScope, error) {
	orgs, err := d.Orgs.List(r.Context(), p.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]OrgScope, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, OrgScope{principal: p, sid: sid, org: o, req: orgScopedRequest(r, p, o)})
	}
	return out, nil
}

func orgScopedRequest(r *http.Request, p session.Principal, o *org.Org) *http.Request {
	return r.WithContext(tenant.Into(r.Context(), tenant.Context{OrgID: o.ID, UserID: p.UserID}))
}

// RequireProject resolves the org and project the path names, gating org membership before the project is looked up. SECURITY: membership is org-level by design, so this admits ANY project in an org the caller belongs to — see ../../../docs/multitenancy/request-scope.md.
func (d Deps) RequireProject(w http.ResponseWriter, r *http.Request) (ProjectScope, bool) {
	sc, ok := d.RequireOrg(w, r)
	if !ok {
		return ProjectScope{}, false
	}
	proj, req, ok := d.ProjectScopeFor(w, sc.req, sc.org.ID, sc.req.PathValue("project"))
	if !ok {
		return ProjectScope{}, false
	}
	return ProjectScope{principal: sc.principal, sid: sc.sid, org: sc.org, project: proj, req: req}, true
}

// SECURITY: reads the held set the scope seam bound; an unbound request holds nothing.
func holds(r *http.Request, p authz.Permission) bool {
	return authz.Can(r.Context(), p) == nil
}

//i18n:use error.org_mismatch
func (d Deps) refuseNotManager(w http.ResponseWriter, r *http.Request, err error, msgKey, fallback string) bool {
	switch {
	case org.IsOrgMismatchError(err):
		d.forbidNonManager(w, r, "error.org_mismatch", "This action belongs to a different organization.")
	case authz.IsDeniedError(err):
		d.forbidNonManager(w, r, msgKey, fallback)
	default:
		return false
	}
	return true
}

//i18n:use error_title.not_allowed
func (d Deps) forbidNonManager(w http.ResponseWriter, r *http.Request, msgKey, fallback string) {
	d.ErrorPage(w, r, http.StatusForbidden,
		localizedDefault(r, "error_title.not_allowed", "Not allowed"),
		localizedDefault(r, msgKey, fallback))
}
