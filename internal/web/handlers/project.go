package handlers

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
	slugs "altalune.id/yasaku/slug"
)

//i18n:use project_new.error_not_manager
const projectNewNotManager = "Only an owner or admin can create a project."

// ProjectHandler owns the /projects routes.
type ProjectHandler struct{ Deps }

// NewProjectHandler wires the handler.
func NewProjectHandler(d Deps, projects *project.Service) *ProjectHandler {
	d.Projects = projects
	return &ProjectHandler{Deps: d}
}

// GetList renders /orgs/{org}/projects.
func (h *ProjectHandler) GetList(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	o, r := sc.org, sc.req
	items, err := h.Projects.List(r.Context(), o.ID)
	if err != nil {
		h.LogErr("web project: list", err)
		h.ErrorPage(w, r, http.StatusInternalServerError, pageTitle(r, "error_title.list_failed"), "Could not load projects.", err)
		return
	}
	Render(w, r, templates.ProjectsLayout(
		h.LayoutForOrg(r, pageTitle(r, "projects.title"), o.Slug, "projects"),
		templates.ProjectsView{OrgSlug: o.Slug, Projects: projectSummaries(items), CreateBlocked: h.createBlocked(r, o.ID)},
	))
}

// GetNew renders /orgs/{org}/projects/new.
func (h *ProjectHandler) GetNew(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	if !holds(sc.req, authz.ProjectsWrite) {
		h.forbidNonManager(w, sc.req, "project_new.error_not_manager", projectNewNotManager)
		return
	}
	o, r := sc.org, sc.req
	v := templates.ProjectNewView{OrgSlug: o.Slug, Slug: slugs.Generate()}
	v.Error, v.ErrorCode = h.limitBlocked(r, h.Projects.CreateAllowed(r.Context(), o.ID), "web project: create allowed")
	v.Blocked = v.Error != ""
	Render(w, r, templates.ProjectNewLayout(
		h.LayoutForOrg(r, pageTitle(r, "nav.create_project"), o.Slug, "projects"),
		v,
	))
}

// PostCreate handles POST /orgs/{org}/projects.
func (h *ProjectHandler) PostCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireOrg(w, r)
	if !ok {
		return
	}
	p, sid, o, r := sc.principal, sc.sid, sc.org, sc.req
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	slug := strings.TrimSpace(r.PostForm.Get("slug"))
	name := strings.TrimSpace(r.PostForm.Get("name"))
	created, err := h.Projects.Create(r.Context(), o.ID, slug, name)
	if err != nil {
		if h.refuseNotManager(w, r, err, "project_new.error_not_manager", projectNewNotManager) {
			return
		}
		h.LogErr("web project: create", err)
		msg := err.Error()
		if project.IsAlreadyExistsError(err) {
			msg = "Slug is already taken."
		}
		if m, ok := h.limitMessage(r.Context(), err); ok {
			msg = m
		}
		if slug == "" {
			slug = slugs.Generate()
		}
		Render(w, r, templates.ProjectNewLayout(
			h.LayoutForOrg(r, pageTitle(r, "nav.create_project"), o.Slug, "projects"),
			templates.ProjectNewView{OrgSlug: o.Slug, Slug: slug, Name: name, Error: msg, ErrorCode: ErrorRef(err)},
		))
		return
	}
	updated := p
	updated.ActiveOrgID = o.ID
	updated.ActiveProjectID = created.ID
	if err := h.UpdateSession(r, sid, updated); err != nil {
		h.LogErr("web project: update session", err)
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, projectPath(o.Slug, created.Slug, "/overview")), http.StatusSeeOther) //nolint:gosec // G710: both slugs are validated by their own slug patterns
}

func projectPath(orgSlug, projectSlug, suffix string) string {
	return "/orgs/" + orgSlug + "/projects/" + projectSlug + suffix
}

func projectSummaries(items []*project.Project) []templates.ProjectSummary {
	out := make([]templates.ProjectSummary, 0, len(items))
	for _, p := range items {
		out = append(out, templates.ProjectSummary{ID: p.ID.String(), Slug: p.Slug, Name: p.Name, System: p.System})
	}
	return out
}

// Register wires the project routes onto mux.
func (h *ProjectHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects", h.GetList)
	mux.HandleFunc("GET /orgs/{org}/projects/new", h.GetNew)
	mux.HandleFunc("POST /orgs/{org}/projects", h.PostCreate)
}

func (h *ProjectHandler) createBlocked(r *http.Request, orgID uuid.UUID) string {
	if !holds(r, authz.ProjectsWrite) {
		return ""
	}
	msg, _ := h.limitBlocked(r, h.Projects.CreateAllowed(r.Context(), orgID), "web project: create allowed")
	return msg
}
