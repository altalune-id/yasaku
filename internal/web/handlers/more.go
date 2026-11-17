package handlers

import (
	"net/http"

	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// MoreHandler serves a project's More menu.
type MoreHandler struct{ Deps }

// NewMoreHandler wires the handler.
func NewMoreHandler(d Deps, projects *project.Service) *MoreHandler {
	d.Projects = projects
	return &MoreHandler{Deps: d}
}

// Register wires the More route onto mux.
func (h *MoreHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/more", h.Get)
}

// Get renders the More menu.
func (h *MoreHandler) Get(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.RequireProject(w, r)
	if !ok {
		return
	}
	d := h.LayoutForProject(sc.req, "", sc.org.Slug, sc.project, "more")
	d.Title = pageTitle(sc.req, "nav.more", sc.project.Name)
	Render(w, sc.req, templates.MoreLayout(d))
}
