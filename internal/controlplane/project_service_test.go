package controlplane_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	blogv1 "altalune.id/yasaku/gen/go/blog/v1"
	projectv1 "altalune.id/yasaku/gen/go/project/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
)

type discoveryFixture struct {
	h            *harness
	orgID        uuid.UUID
	foreignOrgID uuid.UUID
	own          *project.Project
	sibling      *project.Project
	foreign      *project.Project
}

func newDiscoveryFixture(t *testing.T, activeProject bool) *discoveryFixture {
	t.Helper()
	ctx := context.Background()

	orgID, foreignOrgID := uuid.New(), uuid.New()
	own, err := project.New(orgID, "own", "Own Project")
	require.NoError(t, err)
	sibling, err := project.New(orgID, "sibling", "Sibling Project")
	require.NoError(t, err)
	foreign, err := project.New(foreignOrgID, "foreign", "Foreign Project")
	require.NoError(t, err)

	p := session.Principal{UserID: uuid.New(), Email: "agent@example.com", ActiveOrgID: orgID}
	if activeProject {
		p.ActiveProjectID = own.ID
	}
	h := newHarness(t, p)

	f := &discoveryFixture{
		h: h, orgID: orgID, foreignOrgID: foreignOrgID,
		own: own, sibling: sibling, foreign: foreign,
	}
	for _, proj := range []*project.Project{own, sibling, foreign} {
		require.NoError(t, h.projs.Save(ctx, proj))
		f.seedPost(t, proj, proj.Slug+"-post")
	}
	return f
}

func (f *discoveryFixture) seedPost(t *testing.T, proj *project.Project, slug string) *blog.Post {
	t.Helper()
	ctx := context.Background()

	cat, err := category.New(proj.OrgID, proj.ID, "News", "")
	require.NoError(t, err)
	require.NoError(t, f.h.cats.Save(ctx, cat))

	post, err := blog.New(proj.OrgID, proj.ID, cat.ID, slug, slug, "body")
	require.NoError(t, err)
	f.h.posts.Seed(post)
	return post
}

func (f *discoveryFixture) listProjects(t *testing.T) (*projectv1.ListProjectsResponse, error) {
	t.Helper()
	req := connect.NewRequest(&projectv1.ListProjectsRequest{})
	withBearer(req.Header())
	resp, err := f.h.projectClient().ListProjects(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (f *discoveryFixture) listPosts(t *testing.T, projectID string) (*blogv1.ListPostsResponse, error) {
	t.Helper()
	req := connect.NewRequest(&blogv1.ListPostsRequest{ProjectId: projectID})
	withBearer(req.Header())
	resp, err := f.h.blogClient().ListPosts(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func appErrorDetail(t *testing.T, err error) *apperrorv1.ErrorDetail {
	t.Helper()
	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr, "the handler must answer in the canonical connect envelope")
	for _, d := range cerr.Details() {
		msg, verr := d.Value()
		require.NoError(t, verr)
		if ed, ok := msg.(*apperrorv1.ErrorDetail); ok {
			return ed
		}
	}
	t.Fatalf("the error carried no apperror.ErrorDetail: %v", err)
	return nil
}

// TestProject_ListProjects_HidesOtherOrgs is the tenant guard the discovery tool rests on. SECURITY: project_list is the one tool that hands a caller project UUIDs, so a row belonging to another org leaking here is a UUID the caller can then pass to every other tool.
func TestProject_ListProjects_HidesOtherOrgs(t *testing.T) {
	f := newDiscoveryFixture(t, true)

	stored, err := f.h.projs.ByID(context.Background(), f.foreign.ID)
	require.NoError(t, err, "the foreign project must be in the store, or this guard hides nothing")
	require.Equal(t, f.foreignOrgID, stored.OrgID)

	msg, err := f.listProjects(t)
	require.NoError(t, err)

	ids := make([]string, 0, len(msg.GetProjects()))
	slugs := make([]string, 0, len(msg.GetProjects()))
	for _, p := range msg.GetProjects() {
		ids = append(ids, p.GetId())
		slugs = append(slugs, p.GetSlug())
		require.Equal(t, f.orgID.String(), p.GetOrgId(),
			"project_list returned a project owned by org %s while the principal is in %s", p.GetOrgId(), f.orgID)
	}

	require.ElementsMatch(t, []string{"own", "sibling"}, slugs,
		"project_list must return exactly the caller's own org's projects; got %v", slugs)
	require.NotContains(t, ids, f.foreign.ID.String(),
		"project_list leaked the foreign org's project UUID, which is the key to every other tool")
	require.NotContains(t, slugs, "foreign",
		"project_list leaked a foreign org's project")
}

// TestBlog_ListPosts_ResolvesTheProject pins the three ways ListPosts arrives at a project: the explicit argument, the principal's active project, and neither.
func TestBlog_ListPosts_ResolvesTheProject(t *testing.T) {
	t.Run("an omitted projectId reads the principal's active project", func(t *testing.T) {
		f := newDiscoveryFixture(t, true)

		msg, err := f.listPosts(t, "")
		require.NoError(t, err)
		require.Len(t, msg.GetPosts(), 1, "the active project holds exactly one seeded post")
		require.Equal(t, f.own.ID.String(), msg.GetPosts()[0].GetProjectId(),
			"an argument-less list read a project the principal is not active in")
		require.Equal(t, "own-post", msg.GetPosts()[0].GetSlug())
	})

	t.Run("an explicit projectId still reads that project", func(t *testing.T) {
		f := newDiscoveryFixture(t, true)

		msg, err := f.listPosts(t, f.sibling.ID.String())
		require.NoError(t, err)
		require.Len(t, msg.GetPosts(), 1)
		require.Equal(t, f.sibling.ID.String(), msg.GetPosts()[0].GetProjectId(),
			"an explicit projectId must win over the principal's active project")
		require.Equal(t, "sibling-post", msg.GetPosts()[0].GetSlug())
	})

	t.Run("an explicit projectId in another org is refused", func(t *testing.T) {
		f := newDiscoveryFixture(t, true)

		_, err := f.listPosts(t, f.foreign.ID.String())
		require.Error(t, err, "a project UUID from another org was readable by naming it")
		require.Equal(t, connect.CodePermissionDenied, connectCode(err))
		require.NotContains(t, err.Error(), "foreign-post",
			"the refusal leaked the foreign project's contents")
	})

	t.Run("no projectId and no active project names the remedy", func(t *testing.T) {
		f := newDiscoveryFixture(t, false)

		_, err := f.listPosts(t, "")
		require.Error(t, err, "an unresolvable project must fail rather than return an empty list")
		require.Equal(t, connect.CodeFailedPrecondition, connectCode(err))

		detail := appErrorDetail(t, err)
		require.Equal(t, apperror.CodeProjectUnresolved, detail.GetCode())
		require.Equal(t, "project_id", detail.GetMeta()["field"])
		require.Contains(t, err.Error(), "project_list",
			"the error must name the tool that resolves it; got %q", err.Error())
	})
}
