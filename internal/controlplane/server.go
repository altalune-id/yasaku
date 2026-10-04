// Package controlplane wires the Connect-RPC handlers into an http.Handler.
package controlplane

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"

	apikeyv1 "altalune.id/yasaku/gen/go/apikey/v1"
	apikeyv1connect "altalune.id/yasaku/gen/go/apikey/v1/apikeyv1connect"
	authv1 "altalune.id/yasaku/gen/go/auth/v1"
	authv1connect "altalune.id/yasaku/gen/go/auth/v1/authv1connect"
	orgv1 "altalune.id/yasaku/gen/go/org/v1"
	orgv1connect "altalune.id/yasaku/gen/go/org/v1/orgv1connect"
	projectv1 "altalune.id/yasaku/gen/go/project/v1"
	projectv1connect "altalune.id/yasaku/gen/go/project/v1/projectv1connect"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/auth"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/controlplane/interceptor"
	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/wallet"
)

// Deps is every domain collaborator the Connect handlers are built from.
type Deps struct {
	Auths    *auth.Service
	Users    *user.Service
	Orgs     *org.Service
	Projects *project.Service
	Invites  *invite.Service

	Ledgers      *ledger.Service
	Wallets      *wallet.Service
	WalletOpen   *wallet.OpenWorkflow
	TxCategories *category.Service
	Periods      *period.Service
	Transactions *transaction.Service
	Reports      *report.Service
	// Opensheet is nil when opensheet.baseURL is empty.
	Opensheet *opensheetsync.Service
}

// Server holds the wired Connect handlers and their runtime configuration.
type Server struct {
	Cfg    *config.Config
	Kernel *platform.Kernel

	Auths    *auth.Service
	Users    *user.Service
	Orgs     *org.Service
	Projects *project.Service
	Invites  *invite.Service

	// APIKeys is set by boot after New returns, mirroring Authn/KeyPrefix below.
	APIKeys *apikey.Service

	AuthSvc    *AuthService
	ProjectSvc *ProjectService
	MemberSvc  *MemberService
	APIKeySvc  *APIKeyService

	WorkspaceSvc   *WorkspaceService
	LedgerSvc      *LedgerService
	WalletSvc      *WalletService
	CategorySvc    *CategoryService
	TransactionSvc *TransactionService
	PeriodSvc      *PeriodService
	ReportSvc      *ReportService
	OpensheetSvc   *OpensheetService

	Authn     authn.Chain
	KeyPrefix string

	OpenAPIEnabled   bool
	OpenAPIBasicAuth *BasicAuth
}

// New builds a Server from every domain service.
func New(cfg *config.Config, kernel *platform.Kernel, d Deps) *Server {
	s := &Server{
		Cfg:      cfg,
		Kernel:   kernel,
		Auths:    d.Auths,
		Users:    d.Users,
		Orgs:     d.Orgs,
		Projects: d.Projects,
		Invites:  d.Invites,

		AuthSvc:    NewAuthService(d.Orgs),
		ProjectSvc: NewProjectService(d.Projects),
		MemberSvc:  NewMemberService(d.Orgs),

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
	if d.Opensheet != nil {
		s.OpensheetSvc = NewOpensheetService(d.Orgs, d.Projects, d.Opensheet)
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
	_ projectv1connect.ProjectServiceHandler    = (*ProjectService)(nil)
	_ orgv1connect.MemberServiceHandler         = (*MemberService)(nil)
	_ apikeyv1connect.APIKeyServiceHandler      = (*APIKeyService)(nil)
	_ yasakuv1connect.WorkspaceServiceHandler   = (*WorkspaceService)(nil)
	_ yasakuv1connect.LedgerServiceHandler      = (*LedgerService)(nil)
	_ yasakuv1connect.WalletServiceHandler      = (*WalletService)(nil)
	_ yasakuv1connect.CategoryServiceHandler    = (*CategoryService)(nil)
	_ yasakuv1connect.TransactionServiceHandler = (*TransactionService)(nil)
	_ yasakuv1connect.PeriodServiceHandler      = (*PeriodService)(nil)
	_ yasakuv1connect.ReportServiceHandler      = (*ReportService)(nil)
	_ yasakuv1connect.OpensheetServiceHandler   = (*OpensheetService)(nil)
)

// Handler mounts the Connect handlers plus OpenAPI endpoints under basePath+"/api".
func (s *Server) Handler(basePath string) http.Handler {
	opts := s.handlerOptions()
	// NOTE: built here, not in New, since s.APIKeys isn't set until after New returns.
	s.APIKeySvc = NewAPIKeyService(s.APIKeys, s.Projects)

	inner := http.NewServeMux()
	for _, m := range []mountedHandler{
		mounted(authv1connect.NewAuthServiceHandler(s.AuthSvc, opts...)),
		mounted(projectv1connect.NewProjectServiceHandler(s.ProjectSvc, opts...)),
		mounted(orgv1connect.NewMemberServiceHandler(s.MemberSvc, opts...)),
		mounted(apikeyv1connect.NewAPIKeyServiceHandler(s.APIKeySvc, opts...)),
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
	if s.OpensheetSvc != nil {
		m := mounted(yasakuv1connect.NewOpensheetServiceHandler(s.OpensheetSvc, opts...))
		inner.Handle(m.path, m.handler)
	}

	if s.OpenAPIEnabled {
		yamlBody, jsonBody := openAPI()
		if len(yamlBody) > 0 {
			guard := openAPIGuard(s.OpenAPIBasicAuth)
			inner.Handle("/openapi.yaml", s.recoverHTTP(guard(openAPIHandler(yamlBody, "application/yaml"))))
			inner.Handle("/openapi.json", s.recoverHTTP(guard(openAPIHandler(jsonBody, "application/json"))))
			inner.Handle("/docs", s.recoverHTTP(guard(docsHandler(basePath+"/api/openapi.yaml"))))
		}
	}

	mount := basePath + "/api"
	outer := http.NewServeMux()
	outer.Handle(mount+"/", http.StripPrefix(mount, inner))
	return outer
}

// MountedProcedures returns every Connect procedure path the handler serves.
func (s *Server) MountedProcedures() []string {
	var opensheet []string
	if s.OpensheetSvc != nil {
		opensheet = serviceProcedures(yasakuv1.File_yasaku_v1_opensheet_proto, "OpensheetService")
	}
	return slices.Concat(
		serviceProcedures(authv1.File_auth_v1_auth_proto, "AuthService"),
		serviceProcedures(projectv1.File_project_v1_project_proto, "ProjectService"),
		serviceProcedures(orgv1.File_org_v1_org_proto, "MemberService"),
		serviceProcedures(apikeyv1.File_apikey_v1_apikey_proto, "APIKeyService"),
		serviceProcedures(yasakuv1.File_yasaku_v1_workspace_proto, "WorkspaceService"),
		serviceProcedures(yasakuv1.File_yasaku_v1_ledger_proto, "LedgerService"),
		serviceProcedures(yasakuv1.File_yasaku_v1_wallet_proto, "WalletService"),
		serviceProcedures(yasakuv1.File_yasaku_v1_category_proto, "CategoryService"),
		serviceProcedures(yasakuv1.File_yasaku_v1_transaction_proto, "TransactionService"),
		serviceProcedures(yasakuv1.File_yasaku_v1_period_proto, "PeriodService"),
		serviceProcedures(yasakuv1.File_yasaku_v1_report_proto, "ReportService"),
		opensheet,
	)
}

type mountedHandler struct {
	path    string
	handler http.Handler
}

func mounted(path string, handler http.Handler) mountedHandler {
	return mountedHandler{path: path, handler: handler}
}

func serviceProcedures(file protoreflect.FileDescriptor, name string) []string {
	svc := file.Services().ByName(protoreflect.Name(name))
	methods := svc.Methods()
	procedures := make([]string, methods.Len())
	for i := range methods.Len() {
		procedures[i] = fmt.Sprintf("/%s/%s", svc.FullName(), methods.Get(i).Name())
	}
	return procedures
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
		translateAuthnErrors(),
		authn.Interceptor(s.Authn, authn.Scheme{Prefix: s.KeyPrefix}, ScopeTable()),
		interceptor.Tenant(),
	)
	return []connect.HandlerOption{
		connect.WithRecover(s.recoverPanic),
		connect.WithInterceptors(ics...),
	}
}

func (s *Server) recoverPanic(ctx context.Context, _ connect.Spec, _ http.Header, p any) error {
	return interceptor.Translate(ctx, fmt.Errorf("panic: %v", p), s.unexpected())
}

func (s *Server) recoverHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			err := fmt.Errorf("panic: %v", p)
			if unexpected := s.unexpected(); unexpected != nil {
				_ = unexpected(r.Context(), "api: unexpected", err)
			}
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) unexpected() apperror.UnexpectedFunc {
	if s.Kernel == nil || s.Kernel.Reporter == nil {
		return nil
	}
	return s.Kernel.Reporter.Unexpected
}
