package controlplane_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	apikeyv1connect "altalune.id/yasaku/gen/go/apikey/v1/apikeyv1connect"
	authv1connect "altalune.id/yasaku/gen/go/auth/v1/authv1connect"
	blogv1connect "altalune.id/yasaku/gen/go/blog/v1/blogv1connect"
	projectv1connect "altalune.id/yasaku/gen/go/project/v1/projectv1connect"
	todov1connect "altalune.id/yasaku/gen/go/todo/v1/todov1connect"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/controlplane"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/authn"
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
	keys   *fakes.APIKey
	ledger *fakes.Ledger
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
	keys := fakes.NewAPIKey()
	ledgers := fakes.NewLedger()

	orgSvc := org.NewService(orgs, capabilities.Capabilities{OrgCreation: true}, log, reporter.Unexpected)
	projectSvc := project.NewService(projs, log, reporter.Unexpected)
	todoSvc := todo.NewService(tds, log, reporter.Unexpected, &fakes.Queue{})
	postSvc := blog.NewService(posts, log, reporter.Unexpected, fakes.UnitOfWork, &fakes.Webhooks{})
	catSvc := category.NewService(cats, log, reporter.Unexpected)
	tagSvc := tag.NewService(tags, log, reporter.Unexpected)

	kernel := &platform.Kernel{
		Log:      log,
		Reporter: reporter,
		Verifier: stubVerifier{principal: p, err: verr},
	}

	srv := controlplane.New(nil, kernel, controlplane.Deps{
		Orgs:     orgSvc,
		Projects: projectSvc,
		Ledgers:  ledger.NewService(ledgers, log, reporter.Unexpected),
	})
	srv.Authn = authn.Chain{apikey.NewAuthenticator(keys, nil, apikey.Scheme{}, fakes.NewMembers()), tokens.NewAuthenticator(kernel.Verifier)}
	srv.APIKeys = apikey.NewService(keys, apikey.Scheme{}, fakes.PermissiveMembers(), fakes.NewOrgProjects(), log, reporter.Unexpected)
	srv.KeyPrefix = apikey.DefaultPrefix

	mux := http.NewServeMux()
	mux.Handle("/", srv.Handler(""))
	ref := srv.ReferenceHandler(
		controlplane.NewTodoService(todoSvc, tds, projectSvc),
		controlplane.NewBlogService(postSvc, catSvc, tagSvc, projectSvc),
	)
	mux.Handle("/api/"+todov1connect.TodoServiceName+"/", ref)
	mux.Handle("/api/"+blogv1connect.BlogServiceName+"/", ref)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return &harness{t: t, server: ts, orgs: orgs, projs: projs, todos: tds, posts: posts, cats: cats, tags: tags, keys: keys, ledger: ledgers}
}

func (h *harness) mintKey(orgID, projectID uuid.UUID, scopes []string, resourceIDs []uuid.UUID) string {
	h.t.Helper()
	k, plaintext, err := apikey.Scheme{}.Mint(orgID, projectID, "test-key", scopes, resourceIDs, nil, time.Now().UTC())
	if err != nil {
		h.t.Fatalf("mint key: %v", err)
	}
	h.keys.Seed(k)
	return plaintext
}

func (h *harness) apikeyClient() apikeyv1connect.APIKeyServiceClient {
	return apikeyv1connect.NewAPIKeyServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func withKey(plaintext string) func(http.Header) {
	return func(hdr http.Header) { hdr.Set("Authorization", "Bearer "+plaintext) }
}

func (h *harness) authClient() todov1connect.TodoServiceClient {
	return todov1connect.NewTodoServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func (h *harness) blogClient() blogv1connect.BlogServiceClient {
	return blogv1connect.NewBlogServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func (h *harness) projectClient() projectv1connect.ProjectServiceClient {
	return projectv1connect.NewProjectServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func (h *harness) ledgerClient() yasakuv1connect.LedgerServiceClient {
	return yasakuv1connect.NewLedgerServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func (h *harness) workspaceClient() yasakuv1connect.WorkspaceServiceClient {
	return yasakuv1connect.NewWorkspaceServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func (h *harness) whoamiClient() authv1connect.AuthServiceClient {
	return authv1connect.NewAuthServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func withBearer(hdr http.Header) {
	hdr.Set("Authorization", "Bearer stub.token.value")
}

func connectCode(err error) connect.Code {
	if err == nil {
		return connect.CodeUnknown
	}
	return connect.CodeOf(err)
}
