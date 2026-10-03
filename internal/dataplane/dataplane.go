// Package dataplane implements S3, the REST data plane over blog posts: the surface an integrator's running product calls directly, authenticated by API key.
package dataplane

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
)

// Posts is the driven port the data plane reads posts through.
type Posts interface {
	BySlug(ctx context.Context, projectID uuid.UUID, slug string) (PostRef, error)
	List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]PostRef, error)
	Create(ctx context.Context, categoryID uuid.UUID, title, slug, body string) (PostRef, error)
	Update(ctx context.Context, id uuid.UUID, title, slug, body string, categoryID uuid.UUID, ifVersion int) (PostRef, error)
	Delete(ctx context.Context, id uuid.UUID, ifVersion int) error
	Publish(ctx context.Context, id uuid.UUID, ifVersion int) (PostRef, error)
	Unpublish(ctx context.Context, id uuid.UUID, ifVersion int) (PostRef, error)
}

// ListOpts filters a collection read, with PublishedOnly set for an uncredentialed caller.
type ListOpts struct {
	PublishedOnly bool
}

// PostRef is the post this surface needs, referenced across the module boundary by id.
type PostRef struct {
	ID         uuid.UUID
	CategoryID uuid.UUID
	Title      string
	Slug       string
	Body       string
	Published  bool
	Version    int
}

// Authorizer authenticates a raw credential and authorizes it against a scope and tenant.
type Authorizer interface {
	Authenticate(ctx context.Context, raw string) (session.Principal, error)
	Authorize(ctx context.Context, raw, scope string, orgID, projectID, resourceID uuid.UUID) (session.Principal, error)
	AuthorizeProject(ctx context.Context, raw, scope string, orgID, projectID uuid.UUID) (session.Principal, error)
}

// Capabilities is the feature-flag snapshot the data plane checks, such as public reads.
type Capabilities = capabilities.Capabilities

// Handler serves S3, the REST data plane over blog posts.
type Handler struct {
	mux      *http.ServeMux
	resolver resolver
	posts    Posts
	authz    Authorizer
	caps     Capabilities
	idem     *idempotencyStore
	log      *slog.Logger
}

// ServeHTTP implements http.Handler. NOTE: R6 fixes the error shape, so an unrouted path answers with the declared envelope, not the mux default.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, pattern := h.mux.Handler(r); pattern != "" {
		h.mux.ServeHTTP(w, r)
		return
	}
	allowed := h.allowedMethods(r)
	if len(allowed) == 0 {
		h.fail(w, r, &NotFoundError{})
		return
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	h.fail(w, r, &MethodNotAllowedError{})
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	if statusFor(err).status >= http.StatusInternalServerError {
		h.log.ErrorContext(r.Context(), "data plane request failed",
			"method", r.Method, "path", r.URL.Path, "error", err)
	}
	writeError(w, err)
}

func (h *Handler) allowedMethods(r *http.Request) []string {
	routable := []string{
		http.MethodGet, http.MethodHead, http.MethodPost,
		http.MethodPut, http.MethodPatch, http.MethodDelete,
	}
	allowed := make([]string, 0, len(routable))
	for _, method := range routable {
		probe := r.Clone(r.Context())
		probe.Method = method
		if _, pattern := h.mux.Handler(probe); pattern != "" {
			allowed = append(allowed, method)
		}
	}
	return allowed
}

// HandlerParams collects Handler's dependencies.
type HandlerParams struct {
	BasePath string
	Orgs     Orgs
	Projects Projects
	Posts    Posts
	Authz    Authorizer
	Caps     Capabilities
	Log      *slog.Logger
}

// NewHandler builds the data plane's REST handler, mounted under BasePath.
func NewHandler(p HandlerParams) http.Handler {
	log := p.Log
	if log == nil {
		log = slog.Default()
	}

	h := &Handler{
		mux:      http.NewServeMux(),
		resolver: resolver{orgs: p.Orgs, projects: p.Projects},
		posts:    p.Posts,
		authz:    p.Authz,
		caps:     p.Caps,
		idem:     newIdempotencyStore(IdempotencyTTL, IdempotencyMaxEntries),
		log:      log.With("module", "dataplane"),
	}

	posts := p.BasePath + "/orgs/{org}/projects/{project}/posts"
	h.mux.HandleFunc("GET "+posts, h.listPosts)
	h.mux.HandleFunc("GET "+posts+"/{slug}", h.getPost)
	h.mux.HandleFunc("POST "+posts, h.createPost)
	h.mux.HandleFunc("PUT "+posts+"/{slug}", h.replacePost)
	h.mux.HandleFunc("PATCH "+posts+"/{slug}", h.patchPost)
	h.mux.HandleFunc("DELETE "+posts+"/{slug}", h.deletePost)
	h.mux.HandleFunc("POST "+posts+"/{slug}/publish", h.publishPost)
	h.mux.HandleFunc("POST "+posts+"/{slug}/unpublish", h.unpublishPost)

	return h
}
