package handlers

import (
	"cmp"
	"net/http"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// HomeHandler serves the root redirect and each org's dashboard.
type HomeHandler struct{ Deps }

// NewHomeHandler wires the handler.
func NewHomeHandler(d Deps, orgs *org.Service, projects *project.Service) *HomeHandler {
	d.Orgs = orgs
	d.Projects = projects
	return &HomeHandler{Deps: d}
}

// Register wires the root redirect and the per-org overview onto mux.
func (h *HomeHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /{$}", h.GetRoot)
	mux.HandleFunc("GET /orgs/{org}", h.GetOverview)
	mux.HandleFunc("GET /orgs/{org}/switch", h.GetSwitch)
}

// GetRoot lands a signed-in caller on their last project, else their only project, else the org list.
func (h *HomeHandler) GetRoot(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.LoadSession(r)
	if !ok || p.UserID == uuid.Nil {
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, h.landingPath(r, p)), http.StatusSeeOther) //nolint:gosec // G710: slugs come from the store and passed their slug patterns on creation
}

// SECURITY: the target is built only from stored slugs of orgs Orgs.List returns, so a stale or foreign session id never lands the caller outside a current membership.
func (h *HomeHandler) landingPath(r *http.Request, p session.Principal) string {
	if h.Orgs == nil || h.Projects == nil {
		return "/orgs"
	}
	orgs, err := h.Orgs.List(r.Context(), p.UserID)
	if err != nil {
		h.LogErr("home: list orgs", err)
		return "/orgs"
	}
	if len(orgs) == 0 {
		return "/orgs/new"
	}
	for _, o := range orgs {
		if path := h.lastProjectPath(r, p, o); path != "" {
			return path
		}
	}
	if len(orgs) == 1 {
		if path := h.onlyProjectPath(r, p, orgs[0]); path != "" {
			return path
		}
	}
	return "/orgs"
}

// GetSwitch lands a member of the path's org on its last project, else its only project, else its overview.
func (h *HomeHandler) GetSwitch(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	path := h.lastProjectPath(sc.req, sc.principal, sc.org)
	if path == "" {
		path = cmp.Or(h.onlyProjectPath(sc.req, sc.principal, sc.org), "/orgs/"+sc.org.Slug)
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, path), http.StatusSeeOther) //nolint:gosec // G710: slugs come from the store and passed their slug patterns on creation
}

// SECURITY: the session's project counts only when the store finds it inside o, so a stale or foreign id never names the target.
func (h *HomeHandler) lastProjectPath(r *http.Request, p session.Principal, o *org.Org) string {
	if o.ID != p.ActiveOrgID || p.ActiveProjectID == uuid.Nil {
		return ""
	}
	pr, err := h.Projects.ByID(orgScopedRequest(r, p, o).Context(), p.ActiveProjectID)
	if err != nil {
		if !project.IsNotFoundError(err) {
			h.LogErr("home: active project", err)
		}
		return ""
	}
	if pr.OrgID != o.ID {
		return ""
	}
	return projectPath(o.Slug, pr.Slug, "/overview")
}

func (h *HomeHandler) onlyProjectPath(r *http.Request, p session.Principal, o *org.Org) string {
	projects, err := h.Projects.List(orgScopedRequest(r, p, o).Context(), o.ID)
	if err != nil {
		h.LogErr("home: list projects", err)
	}
	if len(projects) != 1 {
		return ""
	}
	return projectPath(o.Slug, projects[0].Slug, "/overview")
}

// GetOverview renders /orgs/{org} — that org's dashboard.
func (h *HomeHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	p, sid, o, r := sc.principal, sc.sid, sc.org, sc.req
	h.remember(r, sid, p, o.ID)

	view := templates.DashboardView{
		OrgSlug:      o.Slug,
		OrgName:      o.Name,
		ActiveProjID: p.ActiveProjectID.String(),
	}
	if orgs, err := h.Orgs.List(r.Context(), p.UserID); err == nil {
		for _, x := range orgs {
			view.Orgs = append(view.Orgs, templates.OrgSummary{ID: x.ID.String(), Slug: x.Slug, Name: x.Name, System: x.System})
		}
	}
	if h.Projects != nil {
		projects, err := h.Projects.List(r.Context(), o.ID)
		if err != nil {
			h.LogErr("home: list projects", err)
		}
		for _, pr := range projects {
			view.Projects = append(view.Projects, templates.ProjectSummary{ID: pr.ID.String(), Slug: pr.Slug, Name: pr.Name, System: pr.System})
		}
	}
	Render(w, r, templates.DashboardLayout(h.LayoutForOrg(r, pageTitle(r, "nav.overview", o.Name), o.Slug, "overview"), view))
}

func (h *HomeHandler) remember(r *http.Request, sid string, p session.Principal, orgID uuid.UUID) {
	if p.ActiveOrgID == orgID {
		return
	}
	updated := p
	updated.ActiveOrgID = orgID
	updated.ActiveProjectID = uuid.Nil
	if err := h.UpdateSession(r, sid, updated); err != nil {
		h.LogErr("home: update session", err)
	}
}
