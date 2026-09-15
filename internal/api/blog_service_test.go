package api_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	blogv1 "altalune.id/yasaku/gen/go/blog/v1"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
)

type blogFixture struct {
	h       *harness
	project *project.Project
	cat     *category.Category
	tagA    *tag.Tag
	tagB    *tag.Tag
}

func newBlogFixture(t *testing.T) *blogFixture {
	t.Helper()
	ctx := context.Background()
	orgID := uuid.New()
	h := newHarness(t, session.Principal{UserID: uuid.New(), Email: "a@b", ActiveOrgID: orgID})

	proj, err := project.New(orgID, "p1", "Project 1")
	require.NoError(t, err)
	require.NoError(t, h.projs.Save(ctx, proj))

	cat, err := category.New(orgID, proj.ID, "Engineering", "")
	require.NoError(t, err)
	require.NoError(t, h.cats.Save(ctx, cat))

	tagA, err := tag.New(orgID, proj.ID, "Go", "")
	require.NoError(t, err)
	require.NoError(t, h.tags.Save(ctx, tagA))

	tagB, err := tag.New(orgID, proj.ID, "Postgres", "")
	require.NoError(t, err)
	require.NoError(t, h.tags.Save(ctx, tagB))

	return &blogFixture{h: h, project: proj, cat: cat, tagA: tagA, tagB: tagB}
}

func (f *blogFixture) create(t *testing.T, title, body string, tagIDs []string) *blogv1.Post {
	t.Helper()
	req := connect.NewRequest(&blogv1.CreatePostRequest{
		ProjectId:    f.project.ID.String(),
		CategoryId:   f.cat.ID.String(),
		Title:        title,
		BodyMarkdown: body,
		TagIds:       tagIDs,
	})
	withBearer(req.Header())
	resp, err := f.h.blogClient().CreatePost(context.Background(), req)
	require.NoError(t, err)
	return resp.Msg.GetPost()
}

func TestBlog_CreatePost_ReturnsNestedCategoryAndTags(t *testing.T) {
	f := newBlogFixture(t)

	post := f.create(t, "Hello world", "# Hello\n\nsome body", []string{f.tagA.ID.String(), f.tagB.ID.String()})

	require.Equal(t, "Hello world", post.GetTitle())
	require.Equal(t, "hello-world", post.GetSlug())
	require.Equal(t, "draft", post.GetStatus())
	require.Equal(t, f.project.ID.String(), post.GetProjectId())

	require.NotNil(t, post.GetCategory(), "create must return the nested category")
	require.Equal(t, f.cat.ID.String(), post.GetCategory().GetId())
	require.Equal(t, "Engineering", post.GetCategory().GetName())
	require.Equal(t, "engineering", post.GetCategory().GetSlug())

	require.Len(t, post.GetTags(), 2)
	require.Equal(t, f.tagA.ID.String(), post.GetTags()[0].GetId())
	require.Equal(t, "Go", post.GetTags()[0].GetName())
	require.Equal(t, f.tagB.ID.String(), post.GetTags()[1].GetId())
	require.Equal(t, "Postgres", post.GetTags()[1].GetName())
}

func TestBlog_CreatePost_BodyHTMLIsRenderedAndScriptEscaped(t *testing.T) {
	f := newBlogFixture(t)

	body := "# Title\n\nhello <script>alert('xss')</script> world\n\n[link](javascript:alert(1))"
	post := f.create(t, "Rendered", body, nil)

	require.Equal(t, body, post.GetBodyMarkdown())
	html := post.GetBodyHtml()
	require.NotEmpty(t, html, "body_html must be rendered server-side")
	require.Contains(t, html, "<h1>Title</h1>")
	require.NotContains(t, html, "<script>", "raw HTML must not survive into body_html")
	require.NotContains(t, html, "</script>")
	require.Contains(t, html, "raw HTML omitted")
	require.NotContains(t, strings.ToLower(html), "javascript:")
}

func TestBlog_UpdatePost_EmptyTagIDsClearsSet(t *testing.T) {
	f := newBlogFixture(t)
	client := f.h.blogClient()

	post := f.create(t, "Tagged", "body", []string{f.tagA.ID.String(), f.tagB.ID.String()})
	require.Len(t, post.GetTags(), 2)

	req := connect.NewRequest(&blogv1.UpdatePostRequest{
		PostId:       post.GetId(),
		CategoryId:   f.cat.ID.String(),
		Title:        "Tagged",
		Slug:         post.GetSlug(),
		BodyMarkdown: "body",
	})
	withBearer(req.Header())
	resp, err := client.UpdatePost(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, resp.Msg.GetPost().GetTags(), "absent tag_ids must clear the set")

	getReq := connect.NewRequest(&blogv1.GetPostRequest{PostId: post.GetId()})
	withBearer(getReq.Header())
	got, err := client.GetPost(context.Background(), getReq)
	require.NoError(t, err)
	require.Empty(t, got.Msg.GetPost().GetTags())
}

func TestBlog_UpdatePost_EmptyTitleIsInvalidArgument(t *testing.T) {
	f := newBlogFixture(t)
	post := f.create(t, "Tagged", "body", nil)

	req := connect.NewRequest(&blogv1.UpdatePostRequest{
		PostId:       post.GetId(),
		CategoryId:   f.cat.ID.String(),
		Title:        "",
		BodyMarkdown: "body",
	})
	withBearer(req.Header())
	_, err := f.h.blogClient().UpdatePost(context.Background(), req)
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connectCode(err))
}

func TestBlog_PublishPost_TwicePreservesFirstPublishedAt(t *testing.T) {
	f := newBlogFixture(t)
	client := f.h.blogClient()

	post := f.create(t, "Lifecycle", "body", nil)
	require.Nil(t, post.GetFirstPublishedAt())

	publish := func() *blogv1.Post {
		req := connect.NewRequest(&blogv1.PublishPostRequest{PostId: post.GetId()})
		withBearer(req.Header())
		resp, err := client.PublishPost(context.Background(), req)
		require.NoError(t, err)
		return resp.Msg.GetPost()
	}

	first := publish()
	require.Equal(t, "published", first.GetStatus())
	require.NotNil(t, first.GetFirstPublishedAt())

	second := publish()
	require.Equal(t, "published", second.GetStatus())
	require.NotNil(t, second.GetFirstPublishedAt())
	require.Equal(t, first.GetFirstPublishedAt().AsTime(), second.GetFirstPublishedAt().AsTime())

	unpubReq := connect.NewRequest(&blogv1.UnpublishPostRequest{PostId: post.GetId()})
	withBearer(unpubReq.Header())
	unpub, err := client.UnpublishPost(context.Background(), unpubReq)
	require.NoError(t, err)
	require.Equal(t, "draft", unpub.Msg.GetPost().GetStatus())
	require.Equal(t, first.GetFirstPublishedAt().AsTime(), unpub.Msg.GetPost().GetFirstPublishedAt().AsTime())
}

func TestBlog_ListPosts_FiltersByStatus(t *testing.T) {
	f := newBlogFixture(t)
	client := f.h.blogClient()

	draft := f.create(t, "Draft one", "body", nil)
	published := f.create(t, "Published one", "body", nil)

	pubReq := connect.NewRequest(&blogv1.PublishPostRequest{PostId: published.GetId()})
	withBearer(pubReq.Header())
	_, err := client.PublishPost(context.Background(), pubReq)
	require.NoError(t, err)

	listReq := connect.NewRequest(&blogv1.ListPostsRequest{
		ProjectId: f.project.ID.String(),
		Status:    "published",
	})
	withBearer(listReq.Header())
	resp, err := client.ListPosts(context.Background(), listReq)
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetPosts(), 1)
	require.Equal(t, published.GetId(), resp.Msg.GetPosts()[0].GetId())
	require.NotNil(t, resp.Msg.GetPosts()[0].GetCategory())
	require.NotEqual(t, draft.GetId(), resp.Msg.GetPosts()[0].GetId())

	badReq := connect.NewRequest(&blogv1.ListPostsRequest{ProjectId: f.project.ID.String(), Status: "archived"})
	withBearer(badReq.Header())
	_, err = client.ListPosts(context.Background(), badReq)
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connectCode(err))
}

func TestBlog_DeletePost_RemovesIt(t *testing.T) {
	f := newBlogFixture(t)
	client := f.h.blogClient()

	post := f.create(t, "Doomed", "body", nil)

	delReq := connect.NewRequest(&blogv1.DeletePostRequest{PostId: post.GetId()})
	withBearer(delReq.Header())
	_, err := client.DeletePost(context.Background(), delReq)
	require.NoError(t, err)

	getReq := connect.NewRequest(&blogv1.GetPostRequest{PostId: post.GetId()})
	withBearer(getReq.Header())
	_, err = client.GetPost(context.Background(), getReq)
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connectCode(err))
}

func TestBlog_CreatePost_CrossTenantProjectIsDenied(t *testing.T) {
	f := newBlogFixture(t)

	foreign, err := project.New(uuid.New(), "p2", "Foreign")
	require.NoError(t, err)
	require.NoError(t, f.h.projs.Save(context.Background(), foreign))

	req := connect.NewRequest(&blogv1.CreatePostRequest{
		ProjectId:  foreign.ID.String(),
		CategoryId: f.cat.ID.String(),
		Title:      "Nope",
	})
	withBearer(req.Header())
	_, err = f.h.blogClient().CreatePost(context.Background(), req)
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connectCode(err))
}

func TestBlog_CreatePost_MissingAuthIsUnauthenticated(t *testing.T) {
	f := newBlogFixture(t)

	req := connect.NewRequest(&blogv1.CreatePostRequest{
		ProjectId:  f.project.ID.String(),
		CategoryId: f.cat.ID.String(),
		Title:      "Nope",
	})
	_, err := f.h.blogClient().CreatePost(context.Background(), req)
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connectCode(err))
}
