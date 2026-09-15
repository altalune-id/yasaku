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
	"altalune.id/yasaku/internal/auth"
	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/blog/category"
	blogtag "altalune.id/yasaku/internal/blog/tag"
	i18npkg "altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/onboard"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/todo"
	"altalune.id/yasaku/internal/user"
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

func buildWebHandler(
	cfg *config.Config,
	kernel *platform.Kernel,
	caps capabilities.Capabilities,
	slogger *slog.Logger,
	reporter *apperror.Reporter,
	healthOK func() bool,
	auths *auth.Service,
	users *user.Service,
	orgs *org.Service,
	projects *project.Service,
	todos *todo.Service,
	invites *invite.Service,
	onboards *onboard.Service,
	posts *blog.Service,
	cats *category.Service,
	tags *blogtag.Service,
	required *atomic.Bool,
	setupToken string,
	apiHandler http.Handler,
	bundle *i18npkg.Bundle,
	defaultLoc i18npkg.Locale,
) http.Handler {
	deps := newWebDeps(cfg, caps, kernel.Sessions, slogger)
	deps.Orgs = orgs
	deps.Projects = projects
	deps.I18n = bundle

	authHandler := webhandlers.NewAuthHandler(deps, auths, users, orgs, projects, kernel.AltAuth, required)
	onboardingHandler := webhandlers.NewOnboardingHandler(deps, users)
	onboardHandler := webhandlers.NewOnboardHandler(deps, users, orgs, projects, onboards, required, setupToken)
	homeHandler := webhandlers.NewHomeHandler(deps, orgs, projects)
	orgHandler := webhandlers.NewOrgHandler(deps, orgs)
	projectHandler := webhandlers.NewProjectHandler(deps, projects)
	todoHandler := webhandlers.NewTodoHandler(deps, projects, todos)
	blogHandler := webhandlers.NewBlogHandler(deps, projects, posts, cats, tags)
	inviteHandler := webhandlers.NewInviteHandler(deps, orgs, invites)
	localeHandler := webhandlers.NewLocaleHandler(deps, users)
	welcomeHandler := webhandlers.NewWelcomeHandler(deps, users)
	signupHandler := webhandlers.NewSignupHandler(deps, users, orgs, projects)
	legalHandler := webhandlers.NewLegalHandler(deps)

	errTmpl := webmw.LogError{Log: slogger}

	return web.NewServer(web.ServerOpts{
		BasePath: cfg.HTTP.BasePath,
		HealthOK: healthOK,
		AppHandlers: []web.Register{
			authHandler, onboardingHandler, onboardHandler, homeHandler, orgHandler, projectHandler, todoHandler, blogHandler, inviteHandler, localeHandler, welcomeHandler, signupHandler, legalHandler,
		},
		APIHandler: apiHandler,
		RobotsCfg:  &struct{ RobotsTxt string }{RobotsTxt: cfg.HTTP.RobotsTxt},
		Middlewares: []web.Middleware{
			webmw.RequestID,
			webmw.RequestLog(slogger),
			webmw.OTel,
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
