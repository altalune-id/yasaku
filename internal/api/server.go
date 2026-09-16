// Package api wires the Connect-RPC handlers into an http.Handler.
package api

import (
	"net/http"

	"connectrpc.com/connect"

	authv1connect "altalune.id/yasaku/gen/go/auth/v1/authv1connect"
	blogv1connect "altalune.id/yasaku/gen/go/blog/v1/blogv1connect"
	todov1connect "altalune.id/yasaku/gen/go/todo/v1/todov1connect"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/api/interceptor"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/auth"
	"altalune.id/yasaku/internal/blog"
	blogcategory "altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/todo"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
)

// Deps is every domain collaborator the Connect handlers are built from.
type Deps struct {
	Auths     *auth.Service
	Users     *user.Service
	UserStore user.Store
	Orgs      *org.Service
	Projects  *project.Service
	Todos     *todo.Service
	TodoStore todo.Store
	Invites   *invite.Service

	Posts    *blog.Service
	BlogCats *blogcategory.Service
	Tags     *tag.Service

	Ledgers      *ledger.Service
	Wallets      *wallet.Service
	WalletOpen   *wallet.OpenWorkflow
	TxCategories *category.Service
	Periods      *period.Service
	Transactions *transaction.Service
	Reports      *report.Service
}

// Server holds the wired Connect handlers and their runtime configuration.
type Server struct {
	Cfg    *config.Config
	Kernel *platform.Kernel

	Deps Deps

	AuthSvc *AuthService
	TodoSvc *TodoService
	BlogSvc *BlogService

	WorkspaceSvc   *WorkspaceService
	LedgerSvc      *LedgerService
	WalletSvc      *WalletService
	CategorySvc    *CategoryService
	TransactionSvc *TransactionService
	PeriodSvc      *PeriodService
	ReportSvc      *ReportService

	OpenAPIEnabled   bool
	OpenAPIBasicAuth *BasicAuth
}

// New builds a Server from every domain service.
func New(cfg *config.Config, kernel *platform.Kernel, d Deps) *Server {
	s := &Server{
		Cfg:    cfg,
		Kernel: kernel,
		Deps:   d,

		AuthSvc: NewAuthService(d.Orgs),
		TodoSvc: NewTodoService(d.Todos, d.TodoStore, d.Projects),
		BlogSvc: NewBlogService(d.Posts, d.BlogCats, d.Tags, d.Projects),

		WorkspaceSvc: NewWorkspaceService(d.Orgs, d.Projects, d.Ledgers, d.Periods),
		LedgerSvc:    NewLedgerService(d.Orgs, d.Projects, d.Ledgers),
		WalletSvc: NewWalletService(d.Orgs, d.Projects, d.Wallets, d.WalletOpen,
			d.Transactions, d.TxCategories, d.Periods, d.Reports, d.Ledgers),
		CategorySvc: NewCategoryService(d.Orgs, d.Projects, d.TxCategories),
		TransactionSvc: NewTransactionService(d.Orgs, d.Projects, d.Transactions,
			d.Wallets, d.TxCategories, d.Periods, d.Ledgers),
		PeriodSvc: NewPeriodService(d.Orgs, d.Projects, d.Periods, d.Reports, d.Ledgers),
		ReportSvc: NewReportService(d.Orgs, d.Projects, d.Reports, d.Periods),
	}
	if cfg != nil {
		s.OpenAPIEnabled = cfg.API.OpenAPI.Enabled
		if cfg.API.OpenAPI.RequireBasicAuth {
			s.OpenAPIBasicAuth = &BasicAuth{
				User:     cfg.API.OpenAPI.BasicAuthUser,
				Password: cfg.API.OpenAPI.BasicAuthPassword,
			}
		}
	}
	return s
}

var (
	_ authv1connect.AuthServiceHandler          = (*AuthService)(nil)
	_ todov1connect.TodoServiceHandler          = (*TodoService)(nil)
	_ blogv1connect.BlogServiceHandler          = (*BlogService)(nil)
	_ yasakuv1connect.WorkspaceServiceHandler   = (*WorkspaceService)(nil)
	_ yasakuv1connect.LedgerServiceHandler      = (*LedgerService)(nil)
	_ yasakuv1connect.WalletServiceHandler      = (*WalletService)(nil)
	_ yasakuv1connect.CategoryServiceHandler    = (*CategoryService)(nil)
	_ yasakuv1connect.TransactionServiceHandler = (*TransactionService)(nil)
	_ yasakuv1connect.PeriodServiceHandler      = (*PeriodService)(nil)
	_ yasakuv1connect.ReportServiceHandler      = (*ReportService)(nil)
)

// Handler mounts the Connect handlers plus OpenAPI endpoints under basePath+"/api".
func (s *Server) Handler(basePath string) http.Handler {
	opts := s.handlerOptions()

	inner := http.NewServeMux()
	for _, m := range []mountedHandler{
		mounted(todov1connect.NewTodoServiceHandler(s.TodoSvc, opts...)),
		mounted(authv1connect.NewAuthServiceHandler(s.AuthSvc, opts...)),
		mounted(blogv1connect.NewBlogServiceHandler(s.BlogSvc, opts...)),
		mounted(yasakuv1connect.NewWorkspaceServiceHandler(s.WorkspaceSvc, opts...)),
		mounted(yasakuv1connect.NewLedgerServiceHandler(s.LedgerSvc, opts...)),
		mounted(yasakuv1connect.NewWalletServiceHandler(s.WalletSvc, opts...)),
		mounted(yasakuv1connect.NewCategoryServiceHandler(s.CategorySvc, opts...)),
		mounted(yasakuv1connect.NewTransactionServiceHandler(s.TransactionSvc, opts...)),
		mounted(yasakuv1connect.NewPeriodServiceHandler(s.PeriodSvc, opts...)),
		mounted(yasakuv1connect.NewReportServiceHandler(s.ReportSvc, opts...)),
	} {
		inner.Handle(m.path, m.handler)
	}

	if s.OpenAPIEnabled {
		yamlBody, jsonBody := openAPI()
		if len(yamlBody) > 0 {
			guard := openAPIGuard(s.OpenAPIBasicAuth)
			inner.Handle("/openapi.yaml", guard(openAPIHandler(yamlBody, "application/yaml")))
			inner.Handle("/openapi.json", guard(openAPIHandler(jsonBody, "application/json")))
			inner.Handle("/docs", guard(docsHandler(basePath+"/api/openapi.yaml")))
		}
	}

	mount := basePath + "/api"
	outer := http.NewServeMux()
	outer.Handle(mount+"/", http.StripPrefix(mount, inner))
	return outer
}

type mountedHandler struct {
	path    string
	handler http.Handler
}

func mounted(path string, handler http.Handler) mountedHandler {
	return mountedHandler{path: path, handler: handler}
}

func (s *Server) handlerOptions() []connect.HandlerOption {
	ics := []connect.Interceptor{
		interceptor.RequestID(),
	}
	if otel, err := interceptor.OTel(nil, nil); err == nil && otel != nil {
		ics = append(ics, otel)
	}
	ics = append(ics,
		interceptor.Wrap(s.unexpected()),
		interceptor.Auth(s.verifier()),
		interceptor.Tenant(),
		interceptor.Principal(s.Deps.UserStore),
	)
	return []connect.HandlerOption{connect.WithInterceptors(ics...)}
}

func (s *Server) unexpected() apperror.UnexpectedFunc {
	if s.Kernel == nil || s.Kernel.Reporter == nil {
		return nil
	}
	return s.Kernel.Reporter.Unexpected
}

func (s *Server) verifier() tokens.Verifier {
	if s.Kernel == nil {
		return nil
	}
	return s.Kernel.Verifier
}
