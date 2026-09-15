package boot

import (
	"context"
	"fmt"
	stdlog "log"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"altalune.id/yasaku/internal/api"
	"altalune.id/yasaku/internal/apperror"
	i18npkg "altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
	webhandlers "altalune.id/yasaku/internal/web/handlers"
	webmw "altalune.id/yasaku/internal/web/middleware"
)

func buildAPIHandler(cfg *config.Config, k *platform.Kernel, s *Services) (*api.Server, http.Handler) {
	srv := api.New(cfg, k, s.Auth, s.Users, s.Orgs, s.Projects, s.Todos, s.Invites, s.TodoStore, s.Posts, s.Categories, s.Tags)
	if !cfg.API.Enabled {
		return srv, nil
	}
	h := srv.Handler(cfg.HTTP.BasePath)
	return srv, h
}

type webHandlerDeps struct {
	Cfg        *config.Config
	Kernel     *platform.Kernel
	Caps       capabilities.Capabilities
	Log        *slog.Logger
	Reporter   *apperror.Reporter
	HealthOK   func() bool
	Services   *Services
	Required   *atomic.Bool
	SetupToken string
	APIHandler http.Handler
	MCPHandler http.Handler
	Bundle     *i18npkg.Bundle
	DefaultLoc i18npkg.Locale
}

func buildWebHandler(d webHandlerDeps) http.Handler {
	cfg, kernel, slogger, svcs := d.Cfg, d.Kernel, d.Log, d.Services
	deps := newWebDeps(cfg, d.Caps, kernel.Sessions, slogger)
	deps.Orgs = svcs.Orgs
	deps.Projects = svcs.Projects
	deps.I18n = d.Bundle

	authHandler := webhandlers.NewAuthHandler(deps, svcs.Auth, svcs.Users, svcs.Orgs, svcs.Projects, kernel.AltAuth, d.Required)
	onboardingHandler := webhandlers.NewOnboardingHandler(deps, svcs.Users)
	onboardHandler := webhandlers.NewOnboardHandler(deps, svcs.Users, svcs.Orgs, svcs.Projects, svcs.Onboards, d.Required, d.SetupToken)
	homeHandler := webhandlers.NewHomeHandler(deps, svcs.Orgs, svcs.Projects)
	orgHandler := webhandlers.NewOrgHandler(deps, svcs.Orgs)
	projectHandler := webhandlers.NewProjectHandler(deps, svcs.Projects)
	todoHandler := webhandlers.NewTodoHandler(deps, svcs.Projects, svcs.Todos)
	blogHandler := webhandlers.NewBlogHandler(deps, svcs.Projects, svcs.Posts, svcs.Categories, svcs.Tags)
	inviteHandler := webhandlers.NewInviteHandler(deps, svcs.Orgs, svcs.Invites)
	localeHandler := webhandlers.NewLocaleHandler(deps, svcs.Users)
	welcomeHandler := webhandlers.NewWelcomeHandler(deps, svcs.Users)
	signupHandler := webhandlers.NewSignupHandler(deps, svcs.Users, svcs.Orgs, svcs.Projects)
	legalHandler := webhandlers.NewLegalHandler(deps)

	errTmpl := webmw.LogError{Log: slogger}

	return web.NewServer(web.ServerOpts{
		BasePath: cfg.HTTP.BasePath,
		HealthOK: d.HealthOK,
		AppHandlers: []web.Register{
			authHandler, onboardingHandler, onboardHandler, homeHandler, orgHandler, projectHandler, todoHandler, blogHandler, inviteHandler, localeHandler, welcomeHandler, signupHandler, legalHandler,
		},
		APIHandler: d.APIHandler,
		MCPHandler: d.MCPHandler,
		RobotsCfg:  &struct{ RobotsTxt string }{RobotsTxt: cfg.HTTP.RobotsTxt},
		Middlewares: []web.Middleware{
			webmw.RequestID,
			webmw.RequestLog(slogger),
			webmw.OTel,
			webmw.Recover(d.Reporter.Unexpected, errTmpl),
			webmw.Session(webmw.SessionConfig{
				Store:  kernel.Sessions,
				Secret: []byte(cfg.HTTP.StateSecret),
			}),
			webmw.Tenant,
			i18npkg.Middleware(i18npkg.MiddlewareOpts{
				Bundle:     d.Bundle,
				Default:    d.DefaultLoc,
				UserLookup: sessionLocaleLookup,
			}),
			webhandlers.OnboardingGate(cfg.HTTP.BasePath, d.Required),
			webhandlers.WelcomeGate(cfg.HTTP.BasePath, cfg.Compliance.RequireAcceptance),
		},
	})
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
