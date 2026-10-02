package dataplane

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/authn"
)

type postView struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	CategoryID string `json:"categoryId"`
	Status     string `json:"status"`
	Version    int    `json:"version"`
}

type listView struct {
	Posts []postView `json:"posts"`
}

type request struct {
	ctx   context.Context
	scope scope
	raw   string
}

// SECURITY: the credential is verified before the path is resolved, so a bad key cannot enumerate slugs.
func (h *Handler) begin(w http.ResponseWriter, r *http.Request) (request, bool) {
	raw := authn.CredentialFrom(r)
	if raw != "" {
		if h.authz == nil {
			h.fail(w, r, &UnauthorizedError{})
			return request{}, false
		}
		if _, err := h.authz.Authenticate(r.Context(), raw); err != nil {
			h.fail(w, r, &UnauthorizedError{})
			return request{}, false
		}
	}
	ctx, sc, err := h.resolver.resolve(r.Context(), r.PathValue("org"), r.PathValue("project"))
	if err != nil {
		h.fail(w, r, err)
		return request{}, false
	}
	return request{ctx: ctx, scope: sc, raw: raw}, true
}

// SECURITY: a write demands a credential before the path is resolved, so writes cannot enumerate {org, project} pairs.
func (h *Handler) beginWrite(w http.ResponseWriter, r *http.Request) (request, bool) {
	if authn.CredentialFrom(r) == "" {
		h.fail(w, r, &UnauthorizedError{})
		return request{}, false
	}
	return h.begin(w, r)
}

// SECURITY: an authorization failure reports the masked not-found, and a nil Authorizer denies.
func (h *Handler) authorize(req request, scopeName string, resourceID uuid.UUID) error {
	if h.authz == nil {
		return &NotFoundError{}
	}
	if _, err := h.authz.Authorize(req.ctx, req.raw, scopeName, req.scope.orgID, req.scope.projectID, resourceID); err != nil {
		return &NotFoundError{}
	}
	return nil
}

func (h *Handler) authorizeProject(req request, scopeName string) error {
	if h.authz == nil {
		return &NotFoundError{}
	}
	if _, err := h.authz.AuthorizeProject(req.ctx, req.raw, scopeName, req.scope.orgID, req.scope.projectID); err != nil {
		return &NotFoundError{}
	}
	return nil
}

func (h *Handler) listPosts(w http.ResponseWriter, r *http.Request) {
	req, ok := h.begin(w, r)
	if !ok {
		return
	}
	opts := ListOpts{PublishedOnly: true}
	if req.raw == "" && !h.caps.PublicReads {
		h.fail(w, r, &NotFoundError{})
		return
	}
	if req.raw != "" {
		if err := h.authorizeProject(req, ScopePostsRead); err != nil {
			h.fail(w, r, err)
			return
		}
		opts.PublishedOnly = false
	}
	posts, err := h.posts.List(req.ctx, req.scope.orgID, req.scope.projectID, opts)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := listView{Posts: make([]postView, 0, len(posts))}
	for _, p := range posts {
		out.Posts = append(out.Posts, viewOf(p))
	}
	h.writeJSON(w, r, out)
}

func (h *Handler) getPost(w http.ResponseWriter, r *http.Request) {
	req, ok := h.begin(w, r)
	if !ok {
		return
	}
	post, err := h.posts.BySlug(req.ctx, req.scope.projectID, r.PathValue("slug"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if readErr := h.readable(req, post); readErr != nil {
		h.fail(w, r, readErr)
		return
	}
	tag := etagFor(post.Version)
	w.Header().Set("ETag", tag)
	if matchesETag(r.Header.Get("If-None-Match"), tag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.writeJSON(w, r, viewOf(post))
}

// SECURITY: an uncredentialed read needs PublicReads AND a published post; either failing is the masked not-found, never 403.
func (h *Handler) readable(req request, post PostRef) error {
	if req.raw != "" {
		return h.authorize(req, ScopePostsRead, post.CategoryID)
	}
	if !h.caps.PublicReads || !post.Published {
		return &NotFoundError{}
	}
	return nil
}

func viewOf(p PostRef) postView {
	status := "draft"
	if p.Published {
		status = "published"
	}
	return postView{
		ID:         p.ID.String(),
		Slug:       p.Slug,
		Title:      p.Title,
		Body:       p.Body,
		CategoryID: p.CategoryID.String(),
		Status:     status,
		Version:    p.Version,
	}
}

func etagFor(version int) string {
	return `W/"` + strconv.Itoa(version) + `"`
}

func matchesETag(header, tag string) bool {
	for candidate := range strings.SplitSeq(header, ",") {
		if strings.TrimSpace(candidate) == tag {
			return true
		}
	}
	return false
}

func (h *Handler) writeJSON(w http.ResponseWriter, r *http.Request, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeBytes(w, http.StatusOK, raw)
}

func writeBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

const maxRequestBytes = 1 << 20

type postRequest struct {
	Title      *string `json:"title"`
	Slug       *string `json:"slug"`
	Body       *string `json:"body"`
	CategoryID *string `json:"categoryId"`
}

// SECURITY: a body shape error is held, not answered, so an unauthorized caller cannot tell 400 from the masked not-found.
func (h *Handler) createPost(w http.ResponseWriter, r *http.Request) {
	req, ok := h.beginWrite(w, r)
	if !ok {
		return
	}
	raw, in, shapeErr := decodeBody(r)
	categoryID, idErr := categoryOf(in.CategoryID, uuid.Nil)
	if shapeErr == nil {
		shapeErr = idErr
	}
	if authErr := h.authorize(req, ScopePostsWrite, categoryID); authErr != nil {
		h.fail(w, r, authErr)
		return
	}
	if shapeErr != nil {
		h.fail(w, r, shapeErr)
		return
	}

	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key != "" {
		rec, reserveErr := h.idem.reserve(req.scope.projectID, key, raw)
		if reserveErr != nil {
			h.fail(w, r, reserveErr)
			return
		}
		if rec != nil {
			w.Header().Set("ETag", rec.resp.etag)
			writeBytes(w, rec.resp.status, rec.resp.body)
			return
		}
		// NOTE: releases the reservation unless store() completed it, so a failed create is retryable.
		defer h.idem.release(req.scope.projectID, key)
	}

	post, err := h.posts.Create(req.ctx, categoryID, deref(in.Title), deref(in.Slug), deref(in.Body))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	body, err := json.Marshal(viewOf(post))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	tag := etagFor(post.Version)
	if key != "" {
		h.idem.store(req.scope.projectID, key, raw, idempotencyResponse{
			status: http.StatusCreated,
			etag:   tag,
			body:   body,
		})
	}
	w.Header().Set("ETag", tag)
	writeBytes(w, http.StatusCreated, body)
}

func (h *Handler) replacePost(w http.ResponseWriter, r *http.Request) { h.writePost(w, r, false) }

func (h *Handler) patchPost(w http.ResponseWriter, r *http.Request) { h.writePost(w, r, true) }

// SECURITY: authorization runs before If-Match and the body are validated, so neither becomes an enumeration oracle.
//
//nolint:cyclop,funlen // one linear request pipeline; splitting it scatters the guard order.
func (h *Handler) writePost(w http.ResponseWriter, r *http.Request, patch bool) {
	req, ok := h.beginWrite(w, r)
	if !ok {
		return
	}
	post, err := h.posts.BySlug(req.ctx, req.scope.projectID, r.PathValue("slug"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if authErr := h.authorize(req, ScopePostsWrite, post.CategoryID); authErr != nil {
		h.fail(w, r, authErr)
		return
	}
	ifVersion, err := ifMatchVersion(r.Header.Get("If-Match"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	_, in, err := decodeBody(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	target := post
	if !patch {
		target.Title, target.Body = "", ""
		target.Slug = r.PathValue("slug")
	}
	if in.Title != nil {
		target.Title = *in.Title
	}
	if in.Body != nil {
		target.Body = *in.Body
	}
	if in.Slug != nil {
		target.Slug = *in.Slug
	}
	target.CategoryID, err = categoryOf(in.CategoryID, post.CategoryID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// SECURITY: a move needs the scope on the destination category too.
	if target.CategoryID != post.CategoryID {
		if authErr := h.authorize(req, ScopePostsWrite, target.CategoryID); authErr != nil {
			h.fail(w, r, authErr)
			return
		}
	}

	updated, err := h.posts.Update(req.ctx, post.ID, target.Title, target.Slug, target.Body, target.CategoryID, ifVersion)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", etagFor(updated.Version))
	h.writeJSON(w, r, viewOf(updated))
}

func (h *Handler) deletePost(w http.ResponseWriter, r *http.Request) {
	req, ok := h.beginWrite(w, r)
	if !ok {
		return
	}
	post, err := h.posts.BySlug(req.ctx, req.scope.projectID, r.PathValue("slug"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if authErr := h.authorize(req, ScopePostsAdmin, post.CategoryID); authErr != nil {
		h.fail(w, r, authErr)
		return
	}
	ifVersion, err := ifMatchVersion(r.Header.Get("If-Match"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if delErr := h.posts.Delete(req.ctx, post.ID, ifVersion); delErr != nil {
		h.fail(w, r, delErr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeBody(r *http.Request) ([]byte, postRequest, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes+1))
	if err != nil || len(raw) > maxRequestBytes {
		return nil, postRequest{}, &BadRequestError{}
	}
	var in postRequest
	if len(raw) > 0 {
		if unmarshalErr := json.Unmarshal(raw, &in); unmarshalErr != nil {
			return nil, postRequest{}, &BadRequestError{}
		}
	}
	return raw, in, nil
}

func categoryOf(raw *string, fallback uuid.UUID) (uuid.UUID, error) {
	if raw == nil {
		return fallback, nil
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		return uuid.Nil, &BadRequestError{}
	}
	return id, nil
}

// SECURITY: a missing If-Match is 428, and the version must parse in [1, int32max] so a malformed header never becomes an unconditional write.
func ifMatchVersion(header string) (int, error) {
	tag := strings.TrimSpace(header)
	if tag == "" {
		return 0, &PreconditionRequiredError{}
	}
	tag = strings.Trim(strings.TrimPrefix(tag, "W/"), `"`)
	version, err := strconv.Atoi(tag)
	if err != nil || version < 1 || version > math.MaxInt32 {
		return 0, &BadRequestError{}
	}
	return version, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *Handler) publishPost(w http.ResponseWriter, r *http.Request) { h.setPublished(w, r, true) }

func (h *Handler) unpublishPost(w http.ResponseWriter, r *http.Request) { h.setPublished(w, r, false) }

// SECURITY: a publication flip makes a post anonymously readable, so it runs the full write guard order.
func (h *Handler) setPublished(w http.ResponseWriter, r *http.Request, publish bool) {
	req, ok := h.beginWrite(w, r)
	if !ok {
		return
	}
	post, err := h.posts.BySlug(req.ctx, req.scope.projectID, r.PathValue("slug"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if authErr := h.authorize(req, ScopePostsWrite, post.CategoryID); authErr != nil {
		h.fail(w, r, authErr)
		return
	}
	ifVersion, err := ifMatchVersion(r.Header.Get("If-Match"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	transition := h.posts.Publish
	if !publish {
		transition = h.posts.Unpublish
	}
	updated, err := transition(req.ctx, post.ID, ifVersion)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", etagFor(updated.Version))
	h.writeJSON(w, r, viewOf(updated))
}
