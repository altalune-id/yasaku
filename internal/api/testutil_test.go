package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	authv1connect "altalune.id/yasaku/gen/go/auth/v1/authv1connect"
	blogv1connect "altalune.id/yasaku/gen/go/blog/v1/blogv1connect"
	todov1connect "altalune.id/yasaku/gen/go/todo/v1/todov1connect"
	"altalune.id/yasaku/internal/api"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/todo"
)

type stubVerifier struct {
	principal session.Principal
	err       error
}

func (s stubVerifier) Verify(_ context.Context, _ string) (session.Principal, error) {
	return s.principal, s.err
}

var _ tokens.Verifier = stubVerifier{}

type harness struct {
	t      *testing.T
	server *httptest.Server
	orgs   *fakes.Org
	projs  *fakes.Project
	todos  *fakes.Todo
	posts  *fakes.Blog
	cats   *fakes.Category
	tags   *fakes.Tag
}

func newHarness(t *testing.T, p session.Principal) *harness {
	t.Helper()
	return newHarnessOpts(t, p, nil)
}

func newHarnessOpts(t *testing.T, p session.Principal, verr error) *harness {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)

	orgs := fakes.NewOrg()
	projs := fakes.NewProject()
	tds := fakes.NewTodo()
	posts := fakes.NewBlog()
	cats := fakes.NewCategory()
	tags := fakes.NewTag()

	orgSvc := org.NewService(orgs, capabilities.Capabilities{OrgCreation: true}, log, reporter.Unexpected)
	projectSvc := project.NewService(projs, log, reporter.Unexpected)
	todoSvc := todo.NewService(tds, log, reporter.Unexpected)
	postSvc := blog.NewService(posts, log, reporter.Unexpected)
	catSvc := category.NewService(cats, log, reporter.Unexpected)
	tagSvc := tag.NewService(tags, log, reporter.Unexpected)

	kernel := &platform.Kernel{
		Log:      log,
		Reporter: reporter,
		Verifier: stubVerifier{principal: p, err: verr},
	}

	srv := api.New(
		nil, // cfg — OpenAPI off by default in these tests
		kernel,
		nil, nil,
		orgSvc, projectSvc, todoSvc, nil,
		tds,
		postSvc, catSvc, tagSvc,
	)
	ts := httptest.NewServer(srv.Handler(""))
	t.Cleanup(ts.Close)
	return &harness{t: t, server: ts, orgs: orgs, projs: projs, todos: tds, posts: posts, cats: cats, tags: tags}
}

func (h *harness) authClient() todov1connect.TodoServiceClient {
	return todov1connect.NewTodoServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func (h *harness) blogClient() blogv1connect.BlogServiceClient {
	return blogv1connect.NewBlogServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func (h *harness) whoamiClient() authv1connect.AuthServiceClient {
	return authv1connect.NewAuthServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func withBearer(hdr http.Header) {
	hdr.Set("Authorization", "Bearer stub-token")
}

func connectCode(err error) connect.Code {
	if err == nil {
		return connect.CodeUnknown
	}
	return connect.CodeOf(err)
}
