package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

//i18n:use signup.error.*

// SignupHandler owns /signup/complete — cloud-only workspace bootstrap for new OIDC users without a pre-existing membership.
type SignupHandler struct {
	Deps
	Users    *user.Service
	Orgs     *org.Service
	Projects *project.Service
}

// NewSignupHandler wires the handler.
func NewSignupHandler(d Deps, users *user.Service, orgs *org.Service, projects *project.Service) *SignupHandler {
	d.Orgs = orgs
	d.Projects = projects
	return &SignupHandler{Deps: d, Users: users, Orgs: orgs, Projects: projects}
}

// Register wires the /signup/complete routes onto mux.
func (h *SignupHandler) Register(mux web.Mux) {
	mux.HandleFunc("GET /signup/complete", h.GetSignup)
	mux.HandleFunc("POST /signup/complete", h.PostSignup)
}

// GetSignup renders the signup-complete form.
func (h *SignupHandler) GetSignup(w http.ResponseWriter, r *http.Request) {
	if h.Cfg.Mode != config.ModeCloud {
		h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.not_found"), "")
		return
	}
	p, _, ok := h.LoadSession(r)
	if !ok {
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return
	}
	if h.hasMembership(r, p.UserID) {
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/"), http.StatusSeeOther)
		return
	}
	Render(w, r, templates.SignupCompleteLayout(h.Base(r, pageTitle(r, "title.complete_signup")), h.defaultView(p)))
}

// PostSignup provisions an org and its first project named after the caller, stamps terms, and redirects to the project overview.
//
//nolint:funlen // linear signup flow reads more clearly as one function.
func (h *SignupHandler) PostSignup(w http.ResponseWriter, r *http.Request) {
	if h.Cfg.Mode != config.ModeCloud {
		h.ErrorPage(w, r, http.StatusNotFound, pageTitle(r, "error_title.not_found"), "")
		return
	}
	p, sid, ok := h.LoadSession(r)
	if !ok {
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return
	}
	if h.hasMembership(r, p.UserID) {
		http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, "/"), http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, pageTitle(r, "error_title.bad_request"), "Could not parse form body.")
		return
	}
	view := h.defaultView(p)
	if view.AskDisplayName {
		view.Name = strings.TrimSpace(r.PostForm.Get("name"))
		if view.Name == "" {
			view.FieldErrors["name"] = "signup.error.name_required"
			h.render(w, r, http.StatusUnprocessableEntity, view)
			return
		}
	}
	if view.AskAccept && r.PostForm.Get("accept_terms") != "1" {
		view.Error = "signup.error.accept_terms"
		h.render(w, r, http.StatusUnprocessableEntity, view)
		return
	}

	ctx := r.Context()
	o, err := h.Orgs.Create(ctx, org.CreateRequest{Name: view.Name, OwnerID: p.UserID})
	if err != nil {
		if org.IsInvalidNameError(err) {
			view.FieldErrors["name"] = "signup.error.name_invalid"
			h.render(w, r, http.StatusUnprocessableEntity, view)
			return
		}
		h.LogErr("signup: create org", err)
		view.Error = "signup.error.create_org"
		view.ErrorCode = ErrorRef(err)
		h.render(w, r, http.StatusInternalServerError, view)
		return
	}

	tctx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: p.UserID})
	proj, err := h.Projects.Create(tctx, o.ID, "", view.Name)
	if err != nil {
		h.LogErr("signup: create project", err)
		view.Error = "signup.error.create_project"
		view.ErrorCode = ErrorRef(err)
		h.render(w, r, http.StatusInternalServerError, view)
		return
	}

	if view.AskDisplayName {
		h.rename(ctx, &p, view.Name)
	}
	if view.AskAccept {
		h.acceptTerms(ctx, &p)
	}

	p.ActiveOrgID = o.ID
	p.ActiveProjectID = proj.ID
	if err := h.UpdateSession(r, sid, p); err != nil {
		h.LogErr("signup: update session", err)
	}
	http.Redirect(w, r, web.Path(h.Cfg.HTTP.BasePath, projectPath(o.Slug, proj.Slug, "/overview")), http.StatusSeeOther) //nolint:gosec // G710: both slugs are generated and validated by their slug patterns
}

func (h *SignupHandler) hasMembership(r *http.Request, userID uuid.UUID) bool {
	orgs, err := h.Orgs.List(r.Context(), userID)
	if err != nil {
		h.LogErr("signup: list orgs", err)
		return false
	}
	return len(orgs) > 0
}

func (h *SignupHandler) defaultView(p session.Principal) templates.SignupCompleteView {
	name := strings.TrimSpace(p.Name)
	if !usableWorkspaceName(name) {
		name = ""
	}
	terms, privacy := h.legalURLs()
	return templates.SignupCompleteView{
		Email:          p.Email,
		Name:           name,
		AskDisplayName: name == "",
		AskAccept:      h.Cfg.Compliance.RequireAcceptance && p.TermsAcceptedAt.IsZero(),
		TermsURL:       terms,
		PrivacyURL:     privacy,
		FieldErrors:    map[string]string{},
	}
}

func usableWorkspaceName(name string) bool {
	return org.ValidateName(name) == nil && project.ValidateName(name) == nil
}

func (h *SignupHandler) render(w http.ResponseWriter, r *http.Request, status int, view templates.SignupCompleteView) {
	RenderStatus(w, r, status, templates.SignupCompleteLayout(h.Base(r, pageTitle(r, "title.complete_signup")), view))
}

func (h *SignupHandler) rename(ctx context.Context, p *session.Principal, name string) {
	if err := h.Users.Rename(ctx, p.UserID, name); err != nil {
		h.LogErr("signup: rename", err)
		return
	}
	p.Name = name
}

func (h *SignupHandler) acceptTerms(ctx context.Context, p *session.Principal) {
	if err := h.Users.AcceptTerms(ctx, p.UserID); err != nil {
		h.LogErr("signup: accept terms", err)
		return
	}
	p.TermsAcceptedAt = time.Now().UTC()
}
