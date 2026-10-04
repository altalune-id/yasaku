package boot

import (
	"context"
	"fmt"
	stdlog "log"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/controlplane"
	"altalune.id/yasaku/internal/dataplane"
	i18npkg "altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/ingest"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
	webhandlers "altalune.id/yasaku/internal/web/handlers"
	webmw "altalune.id/yasaku/internal/web/middleware"
)

func buildAPIHandler(cfg *config.Config, k *platform.Kernel, s *Services) (*controlplane.Server, http.Handler) {
	srv := controlplane.New(cfg, k, controlplane.Deps{
		Auths:    s.Auth,
		Users:    s.Users,
		Orgs:     s.Orgs,
		Projects: s.Projects,
		Invites:  s.Invites,

		Ledgers:      s.Ledgers,
		Wallets:      s.Wallets,
		WalletOpen:   s.WalletOpen,
		TxCategories: s.TxCategories,
		Periods:      s.Periods,
		Transactions: s.Transactions,
		Reports:      s.Reports,
		Opensheet:    s.Opensheet,
	})
	srv.Authn = s.Authn
	srv.KeyPrefix = s.KeyAuthn.Scheme().Prefix()
	srv.APIKeys = s.APIKeys
	if !cfg.API.Enabled {
		return srv, nil
	}
	h := srv.Handler(cfg.HTTP.BasePath)
	return srv, h
}

func buildDataHandler(cfg *config.Config, caps capabilities.Capabilities, slogger *slog.Logger, s *Services) http.Handler {
	if !caps.DataPlaneEnabled || !mountBlog {
		return nil
	}
	return dataplane.NewHandler(dataplane.HandlerParams{
		BasePath: web.Path(cfg.HTTP.BasePath, "/api") + "/v1",
		Orgs:     orgServiceForDataplane{svc: s.Orgs},
		Projects: projectServiceForDataplane{svc: s.Projects},
		Posts:    blogServiceForDataplane{svc: s.Posts},
		Authz:    s.KeyAuthn,
		Caps:     caps,
		Log:      slogger,
	})
}

// NOTE: always mounted, so /hooks/ is reserved rather than reaching the console chain.
func buildIngestHandler(cfg *config.Config, log *slog.Logger) http.Handler {
	return ingest.NewHandler(ingest.HandlerParams{
		BasePath: web.Path(cfg.HTTP.BasePath, "/hooks"),
		Log:      log,
	})
}

type webHandlerDeps struct {
	Cfg         *config.Config
	Kernel      *platform.Kernel
	Caps        capabilities.Capabilities
	Log         *slog.Logger
	Reporter    *apperror.Reporter
	HealthOK    func() bool
	Services    *Services
	Required    *atomic.Bool
	OnComplete  func(ctx context.Context)
	SetupToken  string
	APIHandler  http.Handler
	DataHandler http.Handler
	MCP         mcpSurface
	Bundle      *i18npkg.Bundle
	DefaultLoc  i18npkg.Locale
}

func buildWebHandler(d webHandlerDeps) (handler http.Handler, routes []string) { //nolint:nonamedreturns // two return values differ in role
	cfg, kernel, slogger, svcs := d.Cfg, d.Kernel, d.Log, d.Services
	deps := newWebDeps(cfg, d.Caps, kernel.Sessions, slogger)
	deps.Orgs = svcs.Orgs
	deps.Projects = svcs.Projects
	deps.I18n = d.Bundle
	deps.ProjectLocation = svcs.Ledgers.Location

	authHandler := webhandlers.NewAuthHandler(deps, svcs.Auth, svcs.Users, svcs.Orgs, svcs.Projects, kernel.AltAuth, d.Required)
	onboardingHandler := webhandlers.NewOnboardingHandler(deps, svcs.Users)
	onboardHandler := webhandlers.NewOnboardHandler(deps, svcs.Users, svcs.Orgs, svcs.Projects, svcs.Onboards, d.Required, d.OnComplete, d.SetupToken)
	homeHandler := webhandlers.NewHomeHandler(deps, svcs.Orgs, svcs.Projects)
	orgHandler := webhandlers.NewOrgHandler(deps, svcs.Orgs)
	projectHandler := webhandlers.NewProjectHandler(deps, svcs.Projects)
	todoHandler := webhandlers.NewTodoHandler(deps, svcs.Projects, svcs.Todos)
	blogHandler := webhandlers.NewBlogHandler(deps, svcs.Projects, svcs.Posts, svcs.Categories, svcs.Tags)
	apiKeyHandler := webhandlers.NewAPIKeyHandler(deps, svcs.Projects, svcs.APIKeys)
	webhookHandler := webhandlers.NewWebhookHandler(deps, svcs.Projects, svcs.Webhooks)
	inviteHandler := webhandlers.NewInviteHandler(deps, svcs.Orgs, svcs.Invites)
	localeHandler := webhandlers.NewLocaleHandler(deps, svcs.Users)
	welcomeHandler := webhandlers.NewWelcomeHandler(deps, svcs.Users)
	signupHandler := webhandlers.NewSignupHandler(deps, svcs.Users, svcs.Orgs, svcs.Projects)
	legalHandler := webhandlers.NewLegalHandler(deps)

	overviewHandler := webhandlers.NewOverviewHandler(deps, svcs.Projects, svcs.Wallets, svcs.Transactions, svcs.Periods, svcs.Reports, svcs.TxCategories, svcs.Ledgers)
	walletHandler := webhandlers.NewWalletHandler(deps, svcs.Projects, svcs.Wallets, svcs.WalletOpen, svcs.Transactions, svcs.Ledgers)
	txCategoryHandler := webhandlers.NewTxCategoryHandler(deps, svcs.Projects, svcs.TxCategories)
	transactionHandler := webhandlers.NewTransactionHandler(deps, svcs.Projects, svcs.Wallets, svcs.Transactions, svcs.Periods, svcs.TxCategories, svcs.Ledgers)
	periodHandler := webhandlers.NewPeriodHandler(deps, svcs.Projects, svcs.Periods, svcs.Reports, svcs.Ledgers)
	reportHandler := webhandlers.NewReportHandler(deps, svcs.Projects, svcs.Reports, svcs.Periods)
	settingsHandler := webhandlers.NewSettingsHandler(deps, svcs.Projects, svcs.Ledgers)

	errTmpl := webmw.LogError{Log: slogger}

	return web.NewServerWithRoutes(web.ServerOpts{
		BasePath: cfg.HTTP.BasePath,
		HealthOK: d.HealthOK,
		AppHandlers: publishedConsoleHandlers(append([]web.Register{
			authHandler, onboardingHandler, onboardHandler, homeHandler, orgHandler, projectHandler, todoHandler, blogHandler, apiKeyHandler, webhookHandler, inviteHandler, localeHandler, welcomeHandler, signupHandler, legalHandler,
			overviewHandler, walletHandler, txCategoryHandler, transactionHandler, periodHandler, reportHandler, settingsHandler,
		}, yasakuConsoleHandlers(deps, svcs)...)),
		APIHandler:         d.APIHandler,
		DataHandler:        d.DataHandler,
		IngestHandler:      buildIngestHandler(cfg, slogger),
		MCPHandler:         d.MCP.Handler,
		MCPMetadataHandler: d.MCP.Metadata,
		MCPMetadataPath:    d.MCP.MetadataPath,
		MCPChallengeRoutes: d.MCP.ChallengeRoutes,
		RobotsCfg:          &struct{ RobotsTxt string }{RobotsTxt: cfg.HTTP.RobotsTxt},
		Chains:             surfaceChains(cfg, kernel, slogger, d.Reporter, errTmpl, d.Bundle, d.DefaultLoc, d.Required),
	})
}

func surfaceChains(
	cfg *config.Config,
	kernel *platform.Kernel,
	slogger *slog.Logger,
	reporter *apperror.Reporter,
	errTmpl webmw.ErrorTemplate,
	bundle *i18npkg.Bundle,
	defaultLoc i18npkg.Locale,
	required *atomic.Bool,
) web.SurfaceChains {
	edge := []web.Middleware{
		webmw.RequestID,
		webmw.RequestLog(slogger),
		webmw.OTel,
	}
	return web.SurfaceChains{
		Probes: slices.Concat(edge, []web.Middleware{
			webmw.Recover(reporter.Unexpected, nil),
		}),
		Console: slices.Concat(edge, []web.Middleware{
			webmw.CSP(cspOptions(cfg.HTTP.CSP)),
			webmw.Recover(reporter.Unexpected, errTmpl),
			webmw.Session(webmw.SessionConfig{
				Store:  kernel.Sessions,
				Secret: []byte(cfg.HTTP.StateSecret),
			}),
			webmw.Tenant,
			i18npkg.Middleware(i18npkg.MiddlewareOpts{
				Bundle:     bundle,
				Default:    defaultLoc,
				UserLookup: sessionLocaleLookup,
			}),
			webhandlers.OnboardingGate(cfg.HTTP.BasePath, required),
			webhandlers.WelcomeGate(cfg.HTTP.BasePath, cfg.Compliance.RequireAcceptance),
		}),
		Control: edge,
		Data: slices.Concat(edge, []web.Middleware{
			webmw.RecoverJSON(reporter.Unexpected),
		}),
		Ingest: slices.Concat(edge, []web.Middleware{
			webmw.RecoverJSON(reporter.Unexpected),
		}),
		MCP: slices.Concat(edge, []web.Middleware{
			webmw.RecoverJSON(reporter.Unexpected),
		}),
	}
}

func healthOnlyHandler(cfg *config.Config, healthOK func() bool) http.Handler {
	return web.NewServer(web.ServerOpts{
		BasePath:  cfg.HTTP.BasePath,
		HealthOK:  healthOK,
		RobotsCfg: &struct{ RobotsTxt string }{RobotsTxt: cfg.HTTP.RobotsTxt},
	})
}

func buildI18nBundle(cfg *config.Config) (*i18npkg.Bundle, i18npkg.Locale, error) {
	tag := cfg.I18n.DefaultLocale
	if tag == "" {
		tag = string(i18npkg.EnUS)
	}
	tmp := i18npkg.NewEmbeddedBundle(i18npkg.EnUS)
	loc, err := tmp.Parse(tag)
	if err != nil {
		return nil, "", fmt.Errorf("i18n: default locale %q not among embedded locales", tag)
	}
	return i18npkg.NewEmbeddedBundle(loc), loc, nil
}

func sessionLocaleLookup(ctx context.Context) string {
	return session.PrincipalFrom(ctx).Locale
}

func newWebDeps(cfg *config.Config, caps capabilities.Capabilities, sessions session.Store, slogger *slog.Logger) webhandlers.Deps {
	return webhandlers.Deps{
		Cfg:      cfg,
		Caps:     caps,
		Sessions: sessions,
		Logger:   stdlog.New(logSlogWriter{log: slogger}, "", 0),
	}
}

type logSlogWriter struct{ log *slog.Logger }

func (w logSlogWriter) Write(p []byte) (int, error) {
	if w.log != nil {
		w.log.Info(strings.TrimRight(string(p), "\n"))
	}
	return len(p), nil
}

func cspOptions(cfg config.CSPConfig) webmw.CSPOptions {
	return webmw.CSPOptions{
		Enabled:    cfg.Enabled,
		ReportOnly: cfg.ReportOnly,
		ReportURI:  cfg.ReportURI,
	}
}
