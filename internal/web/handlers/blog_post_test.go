package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/web/handlers"
)

// blogFixture is a handlerFixture plus the three blog services and a mux with the blog routes on it.
type blogFixture struct {
	*handlerFixture
	Posts     *blog.Service
	PostStore *fakes.Blog
	Cats      *category.Service
	Tags      *tag.Service
	Mux       *http.ServeMux

	uid      uuid.UUID
	org      uuid.UUID
	project  *project.Project
	category *category.Category
	tagA     *tag.Tag
	tagB     *tag.Tag
}

func newBlogFixture(t *testing.T) *blogFixture {
	t.Helper()
	f := newFixture(t)

	postStore := fakes.NewBlog()
	posts := blog.NewService(postStore, discardLogger(), passthroughUnexpected())
	cats := category.NewService(fakes.NewCategory(), discardLogger(), passthroughUnexpected())
	tags := tag.NewService(fakes.NewTag(), discardLogger(), passthroughUnexpected())

	uid := uuid.New()
	o := f.seedOrg(t, "acme", uid)
	octx := setTenant(context.Background(), o.ID, uid)
	proj, err := f.Projects.Create(octx, o.ID, "alpha", "Alpha")
	require.NoError(t, err)

	pctx := setTenantProject(octx, o.ID, proj.ID, uid)
	c, err := cats.Create(pctx, "Guides", "guides")
	require.NoError(t, err)
	tagA, err := tags.Create(pctx, "Go", "go")
	require.NoError(t, err)
	tagB, err := tags.Create(pctx, "HTMX", "htmx")
	require.NoError(t, err)

	mux := http.NewServeMux()
	handlers.NewBlogHandler(f.Deps, f.Projects, posts, cats, tags).Register(mux)

	return &blogFixture{
		handlerFixture: f,
		Posts:          posts, PostStore: postStore, Cats: cats, Tags: tags, Mux: mux,
		uid: uid, org: o.ID, project: proj, category: c, tagA: tagA, tagB: tagB,
	}
}

func (b *blogFixture) principal() session.Principal {
	return session.Principal{UserID: b.uid, ActiveOrgID: b.org, ActiveProjectID: b.project.ID}
}

func (b *blogFixture) do(t *testing.T, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	b.Mux.ServeHTTP(rec, b.authedRequest(t, method, target, body, b.principal()))
	return rec
}

func (b *blogFixture) tenantCtx() context.Context {
	return setTenantProject(context.Background(), b.org, b.project.ID, b.uid)
}

const blogBase = "/orgs/acme/projects/alpha"

// TestBlogHandler_PostLifecycle drives the whole post surface end to end: the form, create with tags,
// the list, edit, update, publish, unpublish and delete.
func TestBlogHandler_PostLifecycle(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	rec := b.do(t, http.MethodGet, blogBase+"/posts/new", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `name="body"`, "the body field must be a plain textarea so it submits without JavaScript")
	assert.Contains(t, rec.Body.String(), "easymde.min.js", "the post form loads the markdown editor")
	assert.Contains(t, rec.Body.String(), b.category.ID.String(), "the category select is populated")
	assert.Contains(t, rec.Body.String(), b.tagA.ID.String(), "the tag picker is populated")

	form := url.Values{
		"title":       {"Hello World"},
		"slug":        {"hello-world"},
		"category_id": {b.category.ID.String()},
		"body":        {"# Heading\n\nSome *markdown*."},
		"tags":        {b.tagA.ID.String(), b.tagB.ID.String()},
	}
	rec = b.do(t, http.MethodPost, blogBase+"/posts", form.Encode())
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())
	require.Equal(t, blogBase+"/posts", rec.Header().Get("Location"))

	stored, err := b.Posts.List(b.tenantCtx(), blog.ListOpts{})
	require.NoError(t, err)
	require.Len(t, stored, 1)
	post := stored[0]
	require.ElementsMatch(t, []uuid.UUID{b.tagA.ID, b.tagB.ID}, post.TagIDs)
	require.Equal(t, blog.StatusDraft, post.Status)

	rec = b.do(t, http.MethodGet, blogBase+"/posts", "")
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "Hello World")
	assert.Contains(t, body, "Guides", "the list resolves the category name")
	assert.Contains(t, body, "HTMX", "the list resolves the tag names")
	assert.Contains(t, body, "blog.post_status_draft")

	rec = b.do(t, http.MethodGet, blogBase+"/posts/"+post.ID.String()+"/edit", "")
	require.Equal(t, http.StatusOK, rec.Code)
	edit := rec.Body.String()
	assert.Contains(t, edit, "Hello World")
	assert.Contains(t, edit, "# Heading", "the markdown body round-trips into the textarea")
	assert.Contains(t, edit, "<h1>Heading</h1>", "the form ships a server-rendered preview")

	form.Set("title", "Hello Again")
	form.Del("tags")
	form.Add("tags", b.tagA.ID.String())
	rec = b.do(t, http.MethodPost, blogBase+"/posts/"+post.ID.String(), form.Encode())
	require.Equal(t, http.StatusSeeOther, rec.Code, rec.Body.String())

	updated, err := b.Posts.ByID(b.tenantCtx(), post.ID)
	require.NoError(t, err)
	assert.Equal(t, "Hello Again", updated.Title)
	assert.Equal(t, []uuid.UUID{b.tagA.ID}, updated.TagIDs, "the tag set is replaced, not merged")

	rec = b.do(t, http.MethodPost, blogBase+"/posts/"+post.ID.String()+"/publish", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "blog.post_status_published")
	published, err := b.Posts.ByID(b.tenantCtx(), post.ID)
	require.NoError(t, err)
	require.Equal(t, blog.StatusPublished, published.Status)
	require.NotNil(t, published.FirstPublishedAt)

	rec = b.do(t, http.MethodPost, blogBase+"/posts/"+post.ID.String()+"/unpublish", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "blog.post_status_draft")
	drafted, err := b.Posts.ByID(b.tenantCtx(), post.ID)
	require.NoError(t, err)
	require.Equal(t, blog.StatusDraft, drafted.Status)
	require.NotNil(t, drafted.FirstPublishedAt, "unpublishing keeps the first publication time")

	rec = b.do(t, http.MethodPost, blogBase+"/posts/"+post.ID.String()+"/delete", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "blog.posts_empty")
	assert.Equal(t, 0, b.PostStore.Len())
}

// TestBlogHandler_Preview_RendersMarkdownWithoutRawHTML locks the contract behind @templ.Raw in the fragment.
func TestBlogHandler_Preview_RendersMarkdownWithoutRawHTML(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	form := url.Values{"body": {"# Title\n\n<script>alert(1)</script>\n\n[x](javascript:alert(1))"}}
	rec := b.do(t, http.MethodPost, blogBase+"/posts/preview", form.Encode())
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	assert.Contains(t, body, `id="post-preview"`)
	assert.Contains(t, body, "<h1>Title</h1>", "markdown must reach the page as HTML, not as escaped text")
	assert.NotContains(t, body, "<script>", "goldmark runs without WithUnsafe, so raw HTML is dropped")
	assert.NotContains(t, body, "javascript:", "goldmark rewrites javascript: hrefs")
}

// TestBlogHandler_Preview_IsPostOnly keeps the body out of URLs and access logs.
func TestBlogHandler_Preview_IsPostOnly(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	rec := httptest.NewRecorder()
	b.Mux.ServeHTTP(rec, b.authedRequest(t, http.MethodGet, blogBase+"/posts/preview", "", b.principal()))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestBlogHandler_UnknownTagIDShowsAFormError covers the pre-validation that stands in for the
// TagNotFoundError the blog package deliberately does not have.
func TestBlogHandler_UnknownTagIDShowsAFormError(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	form := url.Values{
		"title":       {"Rejected"},
		"category_id": {b.category.ID.String()},
		"body":        {"body"},
		"tags":        {uuid.NewString()},
	}
	rec := b.do(t, http.MethodPost, blogBase+"/posts", form.Encode())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "blog.post_error_tag")
	assert.Contains(t, rec.Body.String(), "Rejected", "the rejected form keeps what was typed")
	assert.Equal(t, 0, b.PostStore.Len(), "no post is written when a submitted tag is unknown")
}

// TestBlogHandler_MissingCategoryShowsAFormError covers the one invariant a required select cannot enforce.
func TestBlogHandler_MissingCategoryShowsAFormError(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	form := url.Values{"title": {"No Category"}, "category_id": {""}, "body": {"body"}}
	rec := b.do(t, http.MethodPost, blogBase+"/posts", form.Encode())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "blog.post_error_category")
	assert.Equal(t, 0, b.PostStore.Len())
}

// TestBlogHandler_PostFromAnotherProjectIs404 locks the project half of the scope check: blog.ByID
// scopes to the org only, so a sibling project's id must not resolve.
func TestBlogHandler_PostFromAnotherProjectIs404(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	octx := setTenant(context.Background(), b.org, b.uid)
	other, err := b.Projects.Create(octx, b.org, "beta", "Beta")
	require.NoError(t, err)
	otherCtx := setTenantProject(octx, b.org, other.ID, b.uid)
	otherCat, err := b.Cats.Create(otherCtx, "Notes", "notes")
	require.NoError(t, err)
	foreign, err := b.Posts.Create(otherCtx, otherCat.ID, "Foreign", "foreign", "body")
	require.NoError(t, err)

	for _, target := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, blogBase + "/posts/" + foreign.ID.String() + "/edit"},
		{http.MethodPost, blogBase + "/posts/" + foreign.ID.String() + "/publish"},
		{http.MethodPost, blogBase + "/posts/" + foreign.ID.String() + "/delete"},
	} {
		rec := b.do(t, target.method, target.path, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, "%s %s", target.method, target.path)
	}

	still, err := b.Posts.ByID(otherCtx, foreign.ID)
	require.NoError(t, err)
	assert.Equal(t, blog.StatusDraft, still.Status, "the foreign post was left alone")
}

// TestBlogHandler_QuickTagReturnsThePickerWithTheNewTagSelected covers the inline tag-creation control.
func TestBlogHandler_QuickTagReturnsThePickerWithTheNewTagSelected(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	form := url.Values{"picker": {"1"}, "name": {"Templ"}, "tags": {b.tagA.ID.String()}}
	rec := b.do(t, http.MethodPost, blogBase+"/tags/quick", form.Encode())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body := rec.Body.String()
	assert.Contains(t, body, `id="post-tag-picker"`, "the quick-add control swaps the picker, not the admin list")
	assert.Contains(t, body, "Templ")
	assert.Contains(t, body, `value="`+b.tagA.ID.String()+`" checked`, "an already-selected tag stays checked")
	assert.NotContains(t, body, `value="`+b.tagB.ID.String()+`" checked`, "an unselected tag stays unchecked")
	assert.Equal(t, 2, strings.Count(body, `" checked class=`), "exactly the kept tag and the new one are checked")

	// The route is idempotent by slug, so a second submit must not create a duplicate.
	rec = b.do(t, http.MethodPost, blogBase+"/tags/quick", form.Encode())
	require.Equal(t, http.StatusOK, rec.Code)
	items, err := b.Tags.List(b.tenantCtx())
	require.NoError(t, err)
	assert.Len(t, items, 3)
}

// TestBlogHandler_SwappedFragmentsKeepTheirProjectURLs locks the fragment layout data: d.ProjectPath
// collapses to /orgs when ActiveOrg is nil, which silently breaks every action in a swapped-in list.
func TestBlogHandler_SwappedFragmentsKeepTheirProjectURLs(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	form := url.Values{
		"title":       {"Linked"},
		"category_id": {b.category.ID.String()},
		"body":        {"body"},
	}
	require.Equal(t, http.StatusSeeOther, b.do(t, http.MethodPost, blogBase+"/posts", form.Encode()).Code)
	stored, err := b.Posts.List(b.tenantCtx(), blog.ListOpts{})
	require.NoError(t, err)
	require.Len(t, stored, 1)
	id := stored[0].ID.String()

	fragments := []struct {
		name string
		path string
		body string
		want string
	}{
		{"post list", blogBase + "/posts/" + id + "/publish", "", blogBase + "/posts/" + id + "/unpublish"},
		{"category list", blogBase + "/categories", "name=Notes", blogBase + "/categories/"},
		{"tag list", blogBase + "/tags", "name=Fresh", blogBase + "/tags/"},
	}
	for _, f := range fragments {
		rec := b.do(t, http.MethodPost, f.path, f.body)
		require.Equal(t, http.StatusOK, rec.Code, f.name)
		assert.Contains(t, rec.Body.String(), f.want, "%s fragment lost its project URLs", f.name)
		assert.NotContains(t, rec.Body.String(), `action="/orgs"`, "%s fragment fell back to /orgs", f.name)
	}
}

// TestBlogHandler_QuickTagStillServesTheTagAdminList keeps the pre-existing caller working.
func TestBlogHandler_QuickTagStillServesTheTagAdminList(t *testing.T) {
	t.Parallel()
	b := newBlogFixture(t)

	rec := b.do(t, http.MethodPost, blogBase+"/tags/quick", url.Values{"name": {"Templ"}}.Encode())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `id="tag-list"`)
}
