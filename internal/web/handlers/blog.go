package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

// BlogHandler owns the project-scoped category and tag screens.
type BlogHandler struct {
	Deps
	Posts      *blog.Service
	Categories *category.Service
	Tags       *tag.Service
}

// NewBlogHandler wires the handler.
func NewBlogHandler(d Deps, projects *project.Service, posts *blog.Service, cats *category.Service, tags *tag.Service) *BlogHandler {
	d.Projects = projects
	return &BlogHandler{Deps: d, Posts: posts, Categories: cats, Tags: tags}
}

// requireProject resolves the org and project the path names, gating membership before any row is read.
func (h *BlogHandler) requireProject(w http.ResponseWriter, r *http.Request) (projectScope, bool) {
	p, sid, ok := h.LoadSession(r)
	if !ok {
		http.Redirect(w, r, ResolveReturnTo(h.Cfg.HTTP.BasePath, "/login"), http.StatusSeeOther)
		return projectScope{}, false
	}
	o, r, ok := h.OrgScopeFor(w, r, p, r.PathValue("org"))
	if !ok {
		return projectScope{}, false
	}
	proj, r, ok := h.ProjectScopeFor(w, r, o.ID, r.PathValue("project"))
	if !ok {
		return projectScope{}, false
	}
	return projectScope{principal: p, sid: sid, org: o, project: proj, req: r}, true
}

// GetCategories renders the category admin page.
func (h *BlogHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	v, err := h.categoriesView(sc, "", uuid.Nil, "")
	if err != nil {
		h.LogErr("web blog: list categories", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load categories.", err)
		return
	}
	Render(w, sc.req, templates.CategoriesLayout(
		h.LayoutForProject(sc.req, "Categories · "+sc.project.Name, sc.org.Slug, sc.project, "categories"),
		v,
	))
}

// PostCategoryCreate adds a category and returns the refreshed list fragment.
func (h *BlogHandler) PostCategoryCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	slug := strings.TrimSpace(r.PostForm.Get("slug"))
	if _, err := h.Categories.Create(sc.req.Context(), name, slug); err != nil {
		h.LogErr("web blog: create category", err)
		h.writeCategoryList(w, sc, categoryErrorKind(err), uuid.Nil, ErrorRef(err))
		return
	}
	h.writeCategoryList(w, sc, "", uuid.Nil, "")
}

// PostCategoryRename renames a category and returns the refreshed list fragment.
func (h *BlogHandler) PostCategoryRename(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if _, err := h.Categories.Rename(sc.req.Context(), c.ID, name); err != nil {
		h.LogErr("web blog: rename category", err)
		h.writeCategoryList(w, sc, categoryErrorKind(err), uuid.Nil, ErrorRef(err))
		return
	}
	h.writeCategoryList(w, sc, "", uuid.Nil, "")
}

// PostCategoryDelete removes a category, re-rendering the list with a banner when posts still use it.
func (h *BlogHandler) PostCategoryDelete(w http.ResponseWriter, r *http.Request) {
	sc, c, ok := h.requireCategory(w, r)
	if !ok {
		return
	}
	if err := h.Categories.Delete(sc.req.Context(), c.ID); err != nil {
		h.LogErr("web blog: delete category", err)
		if category.IsInUseError(err) {
			h.writeCategoryList(w, sc, templates.BlogErrorInUse, c.ID, ErrorRef(err))
			return
		}
		h.writeCategoryList(w, sc, categoryErrorKind(err), uuid.Nil, ErrorRef(err))
		return
	}
	h.writeCategoryList(w, sc, "", uuid.Nil, "")
}

// GetTags renders the tag admin page.
func (h *BlogHandler) GetTags(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	v, err := h.tagsView(sc, "", uuid.Nil, "")
	if err != nil {
		h.LogErr("web blog: list tags", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load tags.", err)
		return
	}
	Render(w, sc.req, templates.TagsLayout(
		h.LayoutForProject(sc.req, "Tags · "+sc.project.Name, sc.org.Slug, sc.project, "tags"),
		v,
	))
}

// PostTagCreate adds a tag and returns the refreshed list fragment.
func (h *BlogHandler) PostTagCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	slug := strings.TrimSpace(r.PostForm.Get("slug"))
	if _, err := h.Tags.Create(sc.req.Context(), name, slug); err != nil {
		h.LogErr("web blog: create tag", err)
		h.writeTagList(w, sc, tagErrorKind(err), uuid.Nil, ErrorRef(err))
		return
	}
	h.writeTagList(w, sc, "", uuid.Nil, "")
}

// PostTagRename renames a tag and returns the refreshed list fragment.
func (h *BlogHandler) PostTagRename(w http.ResponseWriter, r *http.Request) {
	sc, t, ok := h.requireTag(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if _, err := h.Tags.Rename(sc.req.Context(), t.ID, name); err != nil {
		h.LogErr("web blog: rename tag", err)
		h.writeTagList(w, sc, tagErrorKind(err), uuid.Nil, ErrorRef(err))
		return
	}
	h.writeTagList(w, sc, "", uuid.Nil, "")
}

// PostTagDelete removes a tag, re-rendering the list with a banner when posts still carry it.
func (h *BlogHandler) PostTagDelete(w http.ResponseWriter, r *http.Request) {
	sc, t, ok := h.requireTag(w, r)
	if !ok {
		return
	}
	if err := h.Tags.Delete(sc.req.Context(), t.ID); err != nil {
		h.LogErr("web blog: delete tag", err)
		if tag.IsInUseError(err) {
			h.writeTagList(w, sc, templates.BlogErrorInUse, t.ID, ErrorRef(err))
			return
		}
		h.writeTagList(w, sc, tagErrorKind(err), uuid.Nil, ErrorRef(err))
		return
	}
	h.writeTagList(w, sc, "", uuid.Nil, "")
}

// PostTagQuick creates the named tag when the project has none with that slug, then returns the
// refreshed fragment — the post form's tag picker when the caller sent picker=1, else the tag list.
func (h *BlogHandler) PostTagQuick(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	picker := r.PostForm.Get("picker") == "1"
	selected := selectedSet(r.PostForm["tags"])
	name := strings.TrimSpace(r.PostForm.Get("name"))
	created, err := h.Tags.EnsureByName(sc.req.Context(), sc.org.ID, sc.project.ID, name)
	if err != nil {
		h.LogErr("web blog: quick tag", err)
		if picker {
			h.writePostTagPicker(w, sc, selected, tagErrorKind(err), ErrorRef(err))
			return
		}
		h.writeTagList(w, sc, tagErrorKind(err), uuid.Nil, ErrorRef(err))
		return
	}
	if picker {
		selected[created.ID.String()] = true
		h.writePostTagPicker(w, sc, selected, "", "")
		return
	}
	h.writeTagList(w, sc, "", uuid.Nil, "")
}

// requireCategory resolves the project scope from the path, then the category inside it.
func (h *BlogHandler) requireCategory(w http.ResponseWriter, r *http.Request) (projectScope, *category.Category, bool) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return projectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad id", "Malformed category id.")
		return projectScope{}, nil, false
	}
	c, err := h.Categories.ByID(sc.req.Context(), id)
	if err != nil {
		if category.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That category no longer exists.", err)
			return projectScope{}, nil, false
		}
		h.LogErr("web blog: category byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Lookup failed", "Could not load that category.", err)
		return projectScope{}, nil, false
	}
	return sc, c, true
}

// requireTag resolves the project scope from the path, then the tag inside it.
func (h *BlogHandler) requireTag(w http.ResponseWriter, r *http.Request) (projectScope, *tag.Tag, bool) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return projectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad id", "Malformed tag id.")
		return projectScope{}, nil, false
	}
	t, err := h.Tags.ByID(sc.req.Context(), id)
	if err != nil {
		if tag.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That tag no longer exists.")
			return projectScope{}, nil, false
		}
		h.LogErr("web blog: tag byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Lookup failed", "Could not load that tag.", err)
		return projectScope{}, nil, false
	}
	// SECURITY: tag.ByID does not compare the row's scope to the caller's, so a tag from another
	// project would otherwise be renamable through a guessed id. Same 404 as a missing row.
	if t.OrgID != sc.org.ID || t.ProjectID != sc.project.ID {
		h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That tag no longer exists.")
		return projectScope{}, nil, false
	}
	return sc, t, true
}

func (h *BlogHandler) categoriesView(sc projectScope, errKind string, inUseID uuid.UUID, code string) (templates.CategoriesView, error) {
	items, err := h.Categories.List(sc.req.Context())
	if err != nil {
		return templates.CategoriesView{}, err
	}
	counts, err := h.Posts.CountByCategory(sc.req.Context())
	if err != nil {
		h.LogErr("web blog: count posts by category", err)
		counts = nil
	}
	rows := make([]templates.CategoryRow, 0, len(items))
	for _, c := range items {
		rows = append(rows, templates.CategoryRow{
			ID:    c.ID.String(),
			Name:  c.Name,
			Slug:  c.Slug,
			Posts: counts[c.ID],
		})
	}
	return templates.CategoriesView{
		OrgSlug:     sc.org.Slug,
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Items:       rows,
		ErrorKind:   errKind,
		ErrorCount:  counts[inUseID],
		ErrorCode:   code,
	}, nil
}

func (h *BlogHandler) tagsView(sc projectScope, errKind string, inUseID uuid.UUID, code string) (templates.TagsView, error) {
	items, err := h.Tags.List(sc.req.Context())
	if err != nil {
		return templates.TagsView{}, err
	}
	counts, err := h.Posts.CountByTag(sc.req.Context())
	if err != nil {
		h.LogErr("web blog: count posts by tag", err)
		counts = nil
	}
	rows := make([]templates.TagRow, 0, len(items))
	for _, t := range items {
		rows = append(rows, templates.TagRow{
			ID:    t.ID.String(),
			Name:  t.Name,
			Slug:  t.Slug,
			Posts: counts[t.ID],
		})
	}
	return templates.TagsView{
		OrgSlug:     sc.org.Slug,
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Items:       rows,
		ErrorKind:   errKind,
		ErrorCount:  counts[inUseID],
		ErrorCode:   code,
	}, nil
}

// fragmentBase returns a chromeless LayoutData that still names the org, so d.ProjectPath inside a
// swapped-in fragment resolves to the same URLs the full page rendered. Deps.Base alone leaves
// ActiveOrg nil, which collapses every project path to /orgs.
func (h *BlogHandler) fragmentBase(sc projectScope) web.LayoutData {
	d := h.Base(sc.req, "")
	d.ActiveOrg = &web.ActiveOrg{ID: sc.org.ID.String(), Slug: sc.org.Slug, Name: sc.org.Name}
	return d
}

func (h *BlogHandler) writeCategoryList(w http.ResponseWriter, sc projectScope, errKind string, inUseID uuid.UUID, code string) {
	v, err := h.categoriesView(sc, errKind, inUseID, code)
	if err != nil {
		h.LogErr("web blog: list categories", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load categories.", err)
		return
	}
	Render(w, sc.req, templates.CategoryList(h.fragmentBase(sc), v))
}

func (h *BlogHandler) writeTagList(w http.ResponseWriter, sc projectScope, errKind string, inUseID uuid.UUID, code string) {
	v, err := h.tagsView(sc, errKind, inUseID, code)
	if err != nil {
		h.LogErr("web blog: list tags", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load tags.", err)
		return
	}
	Render(w, sc.req, templates.TagList(h.fragmentBase(sc), v))
}

func categoryErrorKind(err error) string {
	switch {
	case category.IsAlreadyExistsError(err):
		return templates.BlogErrorTaken
	case category.IsInvalidNameError(err):
		return templates.BlogErrorInvalid
	case category.IsInUseError(err):
		return templates.BlogErrorInUse
	default:
		return templates.BlogErrorFailed
	}
}

func tagErrorKind(err error) string {
	switch {
	case tag.IsAlreadyExistsError(err):
		return templates.BlogErrorTaken
	case tag.IsInvalidNameError(err):
		return templates.BlogErrorInvalid
	case tag.IsInUseError(err):
		return templates.BlogErrorInUse
	default:
		return templates.BlogErrorFailed
	}
}

// postDraft is the editable state of a post form, read from an existing row or from a rejected submit.
type postDraft struct {
	id         string
	title      string
	slug       string
	body       string
	categoryID string
	tags       map[string]bool
}

func draftFromPost(p *blog.Post) postDraft {
	d := postDraft{
		id:         p.ID.String(),
		title:      p.Title,
		slug:       p.Slug,
		body:       p.BodyMarkdown,
		categoryID: p.CategoryID.String(),
		tags:       make(map[string]bool, len(p.TagIDs)),
	}
	for _, id := range p.TagIDs {
		d.tags[id.String()] = true
	}
	return d
}

func draftFromForm(id string, form url.Values) postDraft {
	return postDraft{
		id:         id,
		title:      strings.TrimSpace(form.Get("title")),
		slug:       strings.TrimSpace(form.Get("slug")),
		body:       form.Get("body"),
		categoryID: strings.TrimSpace(form.Get("category_id")),
		tags:       selectedSet(form["tags"]),
	}
}

func selectedSet(raw []string) map[string]bool {
	out := make(map[string]bool, len(raw))
	for _, s := range raw {
		if v := strings.TrimSpace(s); v != "" {
			out[v] = true
		}
	}
	return out
}

// GetPosts renders the post list page.
func (h *BlogHandler) GetPosts(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	v, err := h.postsView(sc, "", "")
	if err != nil {
		h.LogErr("web blog: list posts", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load posts.", err)
		return
	}
	Render(w, sc.req, templates.PostsLayout(
		h.LayoutForProject(sc.req, "Posts · "+sc.project.Name, sc.org.Slug, sc.project, "posts"),
		v,
	))
}

// GetPostNew renders the empty post form.
func (h *BlogHandler) GetPostNew(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	h.writePostForm(w, sc, postDraft{}, "", "")
}

// GetPostEdit renders the post form filled from an existing row.
func (h *BlogHandler) GetPostEdit(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePost(w, r)
	if !ok {
		return
	}
	h.writePostForm(w, sc, draftFromPost(p), "", "")
}

// PostPostCreate creates a draft post with its tag set and redirects to the list.
func (h *BlogHandler) PostPostCreate(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, r, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	d := draftFromForm("", r.PostForm)
	tagIDs, tagsOK, err := h.resolveTagIDs(sc, r.PostForm["tags"])
	if err != nil {
		h.LogErr("web blog: list tags", err)
		h.writePostForm(w, sc, d, templates.PostErrorFailed, ErrorRef(err))
		return
	}
	if !tagsOK {
		h.writePostForm(w, sc, d, templates.PostErrorTag, "")
		return
	}
	p, err := h.Posts.Create(sc.req.Context(), parseUUID(d.categoryID), d.title, d.slug, d.body)
	if err != nil {
		h.LogErr("web blog: create post", err)
		h.writePostForm(w, sc, d, postErrorKind(err), ErrorRef(err))
		return
	}
	if len(tagIDs) > 0 {
		if _, err := h.Posts.SetTags(sc.req.Context(), p.ID, tagIDs); err != nil {
			h.LogErr("web blog: set post tags", err)
			d.id = p.ID.String()
			h.writePostForm(w, sc, d, postErrorKind(err), ErrorRef(err))
			return
		}
	}
	h.redirectToPosts(w, sc)
}

// PostPostUpdate re-validates an existing post, replaces its tag set and redirects to the list.
func (h *BlogHandler) PostPostUpdate(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePost(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	d := draftFromForm(p.ID.String(), r.PostForm)
	tagIDs, tagsOK, err := h.resolveTagIDs(sc, r.PostForm["tags"])
	if err != nil {
		h.LogErr("web blog: list tags", err)
		h.writePostForm(w, sc, d, templates.PostErrorFailed, ErrorRef(err))
		return
	}
	if !tagsOK {
		h.writePostForm(w, sc, d, templates.PostErrorTag, "")
		return
	}
	if _, err := h.Posts.Update(sc.req.Context(), p.ID, d.title, d.slug, d.body, parseUUID(d.categoryID)); err != nil {
		h.LogErr("web blog: update post", err)
		h.writePostForm(w, sc, d, postErrorKind(err), ErrorRef(err))
		return
	}
	if _, err := h.Posts.SetTags(sc.req.Context(), p.ID, tagIDs); err != nil {
		h.LogErr("web blog: set post tags", err)
		h.writePostForm(w, sc, d, postErrorKind(err), ErrorRef(err))
		return
	}
	h.redirectToPosts(w, sc)
}

// PostPostPublish publishes a post and returns the refreshed list fragment.
func (h *BlogHandler) PostPostPublish(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePost(w, r)
	if !ok {
		return
	}
	if _, err := h.Posts.Publish(sc.req.Context(), p.ID); err != nil {
		h.LogErr("web blog: publish post", err)
		h.writePostList(w, sc, postErrorKind(err), ErrorRef(err))
		return
	}
	h.writePostList(w, sc, "", "")
}

// PostPostUnpublish returns a post to draft and returns the refreshed list fragment.
func (h *BlogHandler) PostPostUnpublish(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePost(w, r)
	if !ok {
		return
	}
	if _, err := h.Posts.Unpublish(sc.req.Context(), p.ID); err != nil {
		h.LogErr("web blog: unpublish post", err)
		h.writePostList(w, sc, postErrorKind(err), ErrorRef(err))
		return
	}
	h.writePostList(w, sc, "", "")
}

// PostPostDelete removes a post and returns the refreshed list fragment.
func (h *BlogHandler) PostPostDelete(w http.ResponseWriter, r *http.Request) {
	sc, p, ok := h.requirePost(w, r)
	if !ok {
		return
	}
	if err := h.Posts.Delete(sc.req.Context(), p.ID); err != nil && !blog.IsNotFoundError(err) {
		h.LogErr("web blog: delete post", err)
		h.writePostList(w, sc, postErrorKind(err), ErrorRef(err))
		return
	}
	h.writePostList(w, sc, "", "")
}

// PostPostPreview renders the submitted markdown body as the preview fragment.
// NOTE: a POST, not a GET — a body can exceed a URL's practical length and must stay out of access logs.
func (h *BlogHandler) PostPostPreview(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad request", "Could not parse form body.")
		return
	}
	// SECURITY: blog.RenderHTML runs goldmark without WithUnsafe, so raw HTML in the body is dropped
	// rather than emitted — templ.Raw in the fragment is therefore safe.
	Render(w, sc.req, templates.PostPreviewFragment(h.fragmentBase(sc), blog.RenderHTML(r.PostForm.Get("body"))))
}

// requirePost resolves the project scope from the path, then the post inside it.
func (h *BlogHandler) requirePost(w http.ResponseWriter, r *http.Request) (projectScope, *blog.Post, bool) {
	sc, ok := h.requireProject(w, r)
	if !ok {
		return projectScope{}, nil, false
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.ErrorPage(w, sc.req, http.StatusBadRequest, "Bad id", "Malformed post id.")
		return projectScope{}, nil, false
	}
	p, err := h.Posts.ByID(sc.req.Context(), id)
	if err != nil {
		if blog.IsNotFoundError(err) {
			h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That post no longer exists.")
			return projectScope{}, nil, false
		}
		h.LogErr("web blog: post byID", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Lookup failed", "Could not load that post.", err)
		return projectScope{}, nil, false
	}
	// SECURITY: blog.ByID scopes to the org but not the project, so a sibling project's post would
	// otherwise be editable through a guessed id. Same 404 as a missing row.
	if p.OrgID != sc.org.ID || p.ProjectID != sc.project.ID {
		h.ErrorPage(w, sc.req, http.StatusNotFound, "Not found", "That post no longer exists.")
		return projectScope{}, nil, false
	}
	return sc, p, true
}

// resolveTagIDs maps the submitted tag ids onto the project's tags, reporting false when one is unknown.
// NOTE: blog has no TagNotFoundError, so an unknown id would otherwise surface as a wrapped FK failure.
func (h *BlogHandler) resolveTagIDs(sc projectScope, raw []string) ([]uuid.UUID, bool, error) {
	items, err := h.Tags.List(sc.req.Context())
	if err != nil {
		return nil, false, err
	}
	known := make(map[uuid.UUID]struct{}, len(items))
	for _, t := range items {
		known[t.ID] = struct{}{}
	}
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		// NOTE: parseUUID yields uuid.Nil for a malformed id, which is never a stored tag id, so a
		// malformed and an unknown id take the same form-error path.
		id := parseUUID(strings.TrimSpace(s))
		if _, found := known[id]; !found {
			return nil, false, nil
		}
		out = append(out, id)
	}
	return out, true, nil
}

func (h *BlogHandler) postsView(sc projectScope, errKind, code string) (templates.PostsView, error) {
	posts, err := h.Posts.List(sc.req.Context(), blog.ListOpts{})
	if err != nil {
		return templates.PostsView{}, err
	}
	// NOTE: one List per lookup table, not one ByID per row.
	catNames, err := h.categoryNames(sc)
	if err != nil {
		return templates.PostsView{}, err
	}
	tagNames, err := h.tagNames(sc)
	if err != nil {
		return templates.PostsView{}, err
	}
	rows := make([]templates.PostRow, 0, len(posts))
	for _, p := range posts {
		names := make([]string, 0, len(p.TagIDs))
		for _, id := range p.TagIDs {
			if n, found := tagNames[id]; found {
				names = append(names, n)
			}
		}
		rows = append(rows, templates.PostRow{
			ID:           p.ID.String(),
			Title:        p.Title,
			Slug:         p.Slug,
			CategoryName: catNames[p.CategoryID],
			TagNames:     names,
			Published:    p.Status == blog.StatusPublished,
		})
	}
	return templates.PostsView{
		OrgSlug:     sc.org.Slug,
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		Items:       rows,
		ErrorKind:   errKind,
		ErrorCode:   code,
	}, nil
}

func (h *BlogHandler) categoryNames(sc projectScope) (map[uuid.UUID]string, error) {
	items, err := h.Categories.List(sc.req.Context())
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]string, len(items))
	for _, c := range items {
		out[c.ID] = c.Name
	}
	return out, nil
}

func (h *BlogHandler) tagNames(sc projectScope) (map[uuid.UUID]string, error) {
	items, err := h.Tags.List(sc.req.Context())
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]string, len(items))
	for _, t := range items {
		out[t.ID] = t.Name
	}
	return out, nil
}

func (h *BlogHandler) postFormView(sc projectScope, d postDraft, errKind, code string) (templates.PostFormView, error) {
	cats, err := h.Categories.List(sc.req.Context())
	if err != nil {
		return templates.PostFormView{}, err
	}
	catOpts := make([]templates.PostCategoryOption, 0, len(cats))
	for _, c := range cats {
		catOpts = append(catOpts, templates.PostCategoryOption{
			ID:       c.ID.String(),
			Name:     c.Name,
			Selected: c.ID.String() == d.categoryID,
		})
	}
	picker, err := h.postTagPicker(sc, d.tags, "", "")
	if err != nil {
		return templates.PostFormView{}, err
	}
	return templates.PostFormView{
		OrgSlug:     sc.org.Slug,
		ProjectSlug: sc.project.Slug,
		ProjectName: sc.project.Name,
		ID:          d.id,
		Title:       d.title,
		Slug:        d.slug,
		Body:        d.body,
		Categories:  catOpts,
		Tags:        picker,
		PreviewHTML: blog.RenderHTML(d.body),
		ErrorKind:   errKind,
		ErrorCode:   code,
	}, nil
}

func (h *BlogHandler) postTagPicker(sc projectScope, selected map[string]bool, errKind, code string) (templates.PostTagPickerView, error) {
	items, err := h.Tags.List(sc.req.Context())
	if err != nil {
		return templates.PostTagPickerView{}, err
	}
	opts := make([]templates.PostTagOption, 0, len(items))
	for _, t := range items {
		opts = append(opts, templates.PostTagOption{
			ID:       t.ID.String(),
			Name:     t.Name,
			Selected: selected[t.ID.String()],
		})
	}
	return templates.PostTagPickerView{
		Options:   opts,
		ErrorKind: errKind,
		ErrorCode: code,
	}, nil
}

func (h *BlogHandler) writePostList(w http.ResponseWriter, sc projectScope, errKind, code string) {
	v, err := h.postsView(sc, errKind, code)
	if err != nil {
		h.LogErr("web blog: list posts", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load posts.", err)
		return
	}
	Render(w, sc.req, templates.PostList(h.fragmentBase(sc), v))
}

func (h *BlogHandler) writePostForm(w http.ResponseWriter, sc projectScope, d postDraft, errKind, code string) {
	v, err := h.postFormView(sc, d, errKind, code)
	if err != nil {
		h.LogErr("web blog: post form", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "Load failed", "Could not load the post editor.", err)
		return
	}
	title := "New post · " + sc.project.Name
	if d.id != "" {
		title = "Edit post · " + sc.project.Name
	}
	Render(w, sc.req, templates.PostFormLayout(
		h.LayoutForProject(sc.req, title, sc.org.Slug, sc.project, "posts"),
		v,
	))
}

func (h *BlogHandler) writePostTagPicker(w http.ResponseWriter, sc projectScope, selected map[string]bool, errKind, code string) {
	v, err := h.postTagPicker(sc, selected, errKind, code)
	if err != nil {
		h.LogErr("web blog: list tags", err)
		h.ErrorPage(w, sc.req, http.StatusInternalServerError, "List failed", "Could not load tags.", err)
		return
	}
	Render(w, sc.req, templates.PostTagPicker(h.fragmentBase(sc), v))
}

func (h *BlogHandler) redirectToPosts(w http.ResponseWriter, sc projectScope) {
	target := web.Path(h.Cfg.HTTP.BasePath, projectPath(sc.org.Slug, sc.project.Slug, "/posts"))
	http.Redirect(w, sc.req, target, http.StatusSeeOther) //nolint:gosec // G710: both slugs come from rows already resolved by their own slug patterns
}

func parseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

func postErrorKind(err error) string {
	switch {
	case blog.IsAlreadyExistsError(err):
		return templates.PostErrorTaken
	case blog.IsInvalidTitleError(err):
		return templates.PostErrorTitle
	case blog.IsInvalidSlugError(err):
		return templates.PostErrorSlug
	case blog.IsInvalidBodyError(err):
		return templates.PostErrorBody
	case blog.IsCategoryRequiredError(err):
		return templates.PostErrorCategory
	default:
		return templates.PostErrorFailed
	}
}

// Register wires the blog post, category and tag routes onto mux.
func (h *BlogHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/posts", h.GetPosts)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/posts/new", h.GetPostNew)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/posts", h.PostPostCreate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/posts/preview", h.PostPostPreview)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/posts/{id}/edit", h.GetPostEdit)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/posts/{id}", h.PostPostUpdate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/posts/{id}/publish", h.PostPostPublish)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/posts/{id}/unpublish", h.PostPostUnpublish)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/posts/{id}/delete", h.PostPostDelete)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/categories", h.GetCategories)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories", h.PostCategoryCreate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/rename", h.PostCategoryRename)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/categories/{id}/delete", h.PostCategoryDelete)
	mux.HandleFunc("GET /orgs/{org}/projects/{project}/tags", h.GetTags)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/tags", h.PostTagCreate)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/tags/quick", h.PostTagQuick)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/tags/{id}/rename", h.PostTagRename)
	mux.HandleFunc("POST /orgs/{org}/projects/{project}/tags/{id}/delete", h.PostTagDelete)
}
