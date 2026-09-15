package api

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	blogv1 "altalune.id/yasaku/gen/go/blog/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
)

// BlogService implements blog.v1.BlogService.
type BlogService struct {
	posts    *blog.Service
	cats     *category.Service
	tags     *tag.Service
	projects *project.Service
}

// NewBlogService binds the handler to its collaborators.
func NewBlogService(posts *blog.Service, cats *category.Service, tags *tag.Service, projects *project.Service) *BlogService {
	return &BlogService{posts: posts, cats: cats, tags: tags, projects: projects}
}

// CreatePost persists a draft post in the request's project and sets its tag set.
func (s *BlogService) CreatePost(ctx context.Context, req *connect.Request[blogv1.CreatePostRequest]) (*connect.Response[blogv1.CreatePostResponse], error) {
	tctx, err := s.scopeToProject(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, err
	}
	catID, err := optionalUUID("category_id", req.Msg.GetCategoryId())
	if err != nil {
		return nil, err
	}
	tagIDs, err := parseUUIDs("tag_ids", req.Msg.GetTagIds())
	if err != nil {
		return nil, err
	}
	p, err := s.posts.Create(tctx, catID, req.Msg.GetTitle(), req.Msg.GetSlug(), req.Msg.GetBodyMarkdown())
	if err != nil {
		return nil, err
	}
	if len(tagIDs) > 0 {
		p, err = s.posts.SetTags(tctx, p.ID, tagIDs)
		if err != nil {
			return nil, err
		}
	}
	msg, err := s.toProto(tctx, p)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&blogv1.CreatePostResponse{Post: msg}), nil
}

// GetPost returns one post with its category and tags resolved.
func (s *BlogService) GetPost(ctx context.Context, req *connect.Request[blogv1.GetPostRequest]) (*connect.Response[blogv1.GetPostResponse], error) {
	tctx, p, err := s.scopeToPost(ctx, req.Msg.GetPostId())
	if err != nil {
		return nil, err
	}
	msg, err := s.toProto(tctx, p)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&blogv1.GetPostResponse{Post: msg}), nil
}

// ListPosts returns the posts in the request's project, optionally filtered by status and category.
func (s *BlogService) ListPosts(ctx context.Context, req *connect.Request[blogv1.ListPostsRequest]) (*connect.Response[blogv1.ListPostsResponse], error) {
	tctx, err := s.scopeToProject(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, err
	}
	opts, err := listOpts(req.Msg.GetStatus(), req.Msg.GetCategoryId())
	if err != nil {
		return nil, err
	}
	items, err := s.posts.List(tctx, opts)
	if err != nil {
		return nil, err
	}
	msgs, err := s.toProtos(tctx, items)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&blogv1.ListPostsResponse{Posts: msgs}), nil
}

// UpdatePost replaces every editable field of the post, including its tag set.
func (s *BlogService) UpdatePost(ctx context.Context, req *connect.Request[blogv1.UpdatePostRequest]) (*connect.Response[blogv1.UpdatePostResponse], error) {
	tctx, p, err := s.scopeToPost(ctx, req.Msg.GetPostId())
	if err != nil {
		return nil, err
	}
	catID, err := optionalUUID("category_id", req.Msg.GetCategoryId())
	if err != nil {
		return nil, err
	}
	tagIDs, err := parseUUIDs("tag_ids", req.Msg.GetTagIds())
	if err != nil {
		return nil, err
	}
	p, err = s.posts.Update(tctx, p.ID, req.Msg.GetTitle(), req.Msg.GetSlug(), req.Msg.GetBodyMarkdown(), catID)
	if err != nil {
		return nil, err
	}
	p, err = s.posts.SetTags(tctx, p.ID, tagIDs)
	if err != nil {
		return nil, err
	}
	msg, err := s.toProto(tctx, p)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&blogv1.UpdatePostResponse{Post: msg}), nil
}

// DeletePost removes the referenced post together with its tag links.
func (s *BlogService) DeletePost(ctx context.Context, req *connect.Request[blogv1.DeletePostRequest]) (*connect.Response[blogv1.DeletePostResponse], error) {
	tctx, p, err := s.scopeToPost(ctx, req.Msg.GetPostId())
	if err != nil {
		return nil, err
	}
	if delErr := s.posts.Delete(tctx, p.ID); delErr != nil {
		return nil, delErr
	}
	return connect.NewResponse(&blogv1.DeletePostResponse{}), nil
}

// PublishPost marks the post published, recording the first publication only once.
func (s *BlogService) PublishPost(ctx context.Context, req *connect.Request[blogv1.PublishPostRequest]) (*connect.Response[blogv1.PublishPostResponse], error) {
	tctx, p, err := s.scopeToPost(ctx, req.Msg.GetPostId())
	if err != nil {
		return nil, err
	}
	p, err = s.posts.Publish(tctx, p.ID)
	if err != nil {
		return nil, err
	}
	msg, err := s.toProto(tctx, p)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&blogv1.PublishPostResponse{Post: msg}), nil
}

// UnpublishPost returns the post to draft, retaining its first publication time.
func (s *BlogService) UnpublishPost(ctx context.Context, req *connect.Request[blogv1.UnpublishPostRequest]) (*connect.Response[blogv1.UnpublishPostResponse], error) {
	tctx, p, err := s.scopeToPost(ctx, req.Msg.GetPostId())
	if err != nil {
		return nil, err
	}
	p, err = s.posts.Unpublish(tctx, p.ID)
	if err != nil {
		return nil, err
	}
	msg, err := s.toProto(tctx, p)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&blogv1.UnpublishPostResponse{Post: msg}), nil
}

func (s *BlogService) scopeToProject(ctx context.Context, projectIDRaw string) (context.Context, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	pid, err := parseUUID("project_id", projectIDRaw)
	if err != nil {
		return nil, err
	}
	scoped := tenant.Into(ctx, tenant.Context{OrgID: p.ActiveOrgID, UserID: p.UserID})
	proj, err := s.projects.ByID(scoped, pid)
	if err != nil {
		return nil, err
	}
	if proj.OrgID != p.ActiveOrgID {
		return nil, forbiddenErr("project belongs to another org", "project_id", pid.String())
	}
	return tenant.Into(ctx, tenant.Context{
		OrgID:     proj.OrgID,
		ProjectID: proj.ID,
		UserID:    p.UserID,
	}), nil
}

func (s *BlogService) scopeToPost(ctx context.Context, postIDRaw string) (context.Context, *blog.Post, error) {
	pr, err := principal(ctx)
	if err != nil {
		return nil, nil, err
	}
	pid, err := parseUUID("post_id", postIDRaw)
	if err != nil {
		return nil, nil, err
	}
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: pr.ActiveOrgID, UserID: pr.UserID})
	post, err := s.posts.ByID(orgCtx, pid)
	if err != nil {
		return nil, nil, err
	}
	if post.OrgID != pr.ActiveOrgID {
		return nil, nil, forbiddenErr("post belongs to another org", "post_id", pid.String())
	}
	return tenant.Into(ctx, tenant.Context{
		OrgID:     post.OrgID,
		ProjectID: post.ProjectID,
		UserID:    pr.UserID,
	}), post, nil
}

func (s *BlogService) toProto(ctx context.Context, p *blog.Post) (*blogv1.Post, error) {
	msgs, err := s.toProtos(ctx, []*blog.Post{p})
	if err != nil {
		return nil, err
	}
	return msgs[0], nil
}

func (s *BlogService) toProtos(ctx context.Context, posts []*blog.Post) ([]*blogv1.Post, error) {
	cats, err := s.cats.List(ctx)
	if err != nil {
		return nil, err
	}
	tgs, err := s.tags.List(ctx)
	if err != nil {
		return nil, err
	}
	catByID := make(map[uuid.UUID]*category.Category, len(cats))
	for _, c := range cats {
		catByID[c.ID] = c
	}
	tagByID := make(map[uuid.UUID]*tag.Tag, len(tgs))
	for _, t := range tgs {
		tagByID[t.ID] = t
	}
	out := make([]*blogv1.Post, 0, len(posts))
	for _, p := range posts {
		out = append(out, postToProto(p, catByID, tagByID))
	}
	return out, nil
}

func postToProto(p *blog.Post, cats map[uuid.UUID]*category.Category, tags map[uuid.UUID]*tag.Tag) *blogv1.Post {
	msg := &blogv1.Post{
		Id:           p.ID.String(),
		ProjectId:    p.ProjectID.String(),
		Title:        p.Title,
		Slug:         p.Slug,
		BodyMarkdown: p.BodyMarkdown,
		BodyHtml:     blog.RenderHTML(p.BodyMarkdown),
		Status:       string(p.Status),
		Tags:         make([]*blogv1.Tag, 0, len(p.TagIDs)),
		CreatedAt:    timestamppb.New(p.CreatedAt),
		UpdatedAt:    timestamppb.New(p.UpdatedAt),
	}
	if c, ok := cats[p.CategoryID]; ok {
		msg.Category = &blogv1.Category{Id: c.ID.String(), Name: c.Name, Slug: c.Slug}
	}
	for _, id := range p.TagIDs {
		t, ok := tags[id]
		if !ok {
			continue
		}
		msg.Tags = append(msg.Tags, &blogv1.Tag{Id: t.ID.String(), Name: t.Name, Slug: t.Slug})
	}
	if p.FirstPublishedAt != nil {
		msg.FirstPublishedAt = timestamppb.New(*p.FirstPublishedAt)
	}
	return msg
}

func listOpts(statusRaw, categoryIDRaw string) (blog.ListOpts, error) {
	var opts blog.ListOpts
	if s := strings.TrimSpace(statusRaw); s != "" {
		st := blog.Status(s)
		if st != blog.StatusDraft && st != blog.StatusPublished {
			return opts, validationErr("status", "status must be draft or published")
		}
		opts.Status = &st
	}
	if strings.TrimSpace(categoryIDRaw) != "" {
		cid, err := parseUUID("category_id", categoryIDRaw)
		if err != nil {
			return opts, err
		}
		opts.CategoryID = &cid
	}
	return opts, nil
}

func validationErr(field, msg string) error {
	return apperror.New(
		apperror.CodeValidation,
		msg,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeValidation,
			Meta: map[string]string{"field": field},
		},
	)
}

func optionalUUID(field, raw string) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, nil
	}
	return parseUUID(field, raw)
}

func parseUUIDs(field string, raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, r := range raw {
		id, err := parseUUID(field, r)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}
