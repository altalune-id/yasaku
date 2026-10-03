// Package boot is the composition root: it wires the platform Kernel plus every domain service under one worker Supervisor.
package boot

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"altalune.id/yasaku/authl"
	"altalune.id/yasaku/httpclient"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/auth"
	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/blog/category"
	"altalune.id/yasaku/internal/blog/tag"
	"altalune.id/yasaku/internal/controlplane"
	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/onboard"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/notify"
	"altalune.id/yasaku/internal/platform/outbox"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/todo"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/internal/webhook"
	"altalune.id/yasaku/logger"
	"altalune.id/yasaku/mailer"
	rootmcp "altalune.id/yasaku/mcp"
	"altalune.id/yasaku/nanoid"
	"altalune.id/yasaku/scheduler"
	"altalune.id/yasaku/telemetry"
	"altalune.id/yasaku/worker"
)

const setupTokenLen = 32

const (
	webhookTimeout       = 10 * time.Second
	webhookResponseLimit = 64 << 10
)

// Server is the fully-wired dependency graph produced by BootServer.
type Server struct {
	Cfg      *config.Config
	Caps     capabilities.Capabilities
	Platform *platform.Kernel

	Auth       *auth.Service
	Users      *user.Service
	Orgs       *org.Service
	Projects   *project.Service
	Todos      *todo.Service
	Invites    *invite.Service
	Onboards   *onboard.Service
	Posts      *blog.Service
	Categories *category.Service
	Tags       *tag.Service
	APIKeys    *apikey.Service
	Webhooks   *webhook.Service

	Onboard *user.OnboardWorkflow

	Onboarded bool

	// SetupToken gates /onboard while onboarding is still required; empty once onboarded.
	SetupToken string

	Web http.Handler
	// Routes are the app-route patterns the web handlers registered, taken from the mux.
	Routes    []string
	API       *controlplane.Server
	MCP       *rootmcp.Server
	Scheduler *scheduler.Runner
	// Consumer runs this instance's queue jobs; nil when it does not run here.
	Consumer *queue.Consumer
	// Listener applies broadcasts to this instance; nil when the queue is disabled.
	Listener   *queue.Listen
	Health     *db.HealthMonitor
	Supervisor *worker.Supervisor

	// CompleteOnboarding clears the setup gate here and on every other running instance.
	CompleteOnboarding func(ctx context.Context)

	httpHandler  http.Handler
	shutdownOTel func(context.Context) error
}

// BootServer builds every dependency and returns a wired [Server].
func BootServer(ctx context.Context, cfg *config.Config, opts ...Option) (*Server, error) {
	o := newOptions()
	for _, opt := range opts {
		opt(o)
	}

	log := o.logger
	if log == nil {
		log = logger.New(cfg.Log)
	}

	tp, mp, shutdownOTel, err := telemetry.Setup(ctx, cfg.Telemetry, log)
	if err != nil {
		return nil, fmt.Errorf("boot: telemetry: %w", err)
	}

	mail, err := mailer.New(mailerConfig(cfg.Mail))
	if err != nil {
		_ = shutdownOTel(context.Background())
		return nil, fmt.Errorf("boot: mailer: %w", err)
	}

	sinks := notify.Build(cfg.Observability.Reporter, mail, log)
	reporter := apperror.NewReporter(log, cfg.Mode.IsProduction(),
		apperror.WithContextMeta(tenant.ContextMeta),
		apperror.WithSinks(sinks...),
	)

	pool, pgConn, err := openDBAndMigrate(ctx, cfg, log)
	if err != nil {
		_ = shutdownOTel(context.Background())
		return nil, err
	}

	verifier, err := tokens.NewVerifier(ctx, cfg.Tokens)
	if err != nil {
		_ = pool.Close()
		_ = shutdownOTel(context.Background())
		return nil, fmt.Errorf("boot: tokens: %w", err)
	}

	altAuth, err := buildAltAuth(ctx, cfg, log)
	if err != nil {
		_ = pool.Close()
		_ = shutdownOTel(context.Background())
		return nil, fmt.Errorf("boot: authl: %w", err)
	}

	sl, err := buildSealer(cfg, log)
	if err != nil {
		_ = pool.Close()
		_ = shutdownOTel(context.Background())
		return nil, err
	}

	sessions := session.NewStore(cfg.DB, pool, sl, reporter.Unexpected)
	caps := capabilities.From(cfg)

	kernel := &platform.Kernel{
		Pool:     pool,
		PgConn:   pgConn,
		Log:      log,
		Reporter: reporter,
		Sessions: sessions,
		Sealer:   sl,
		Verifier: verifier,
		Mail:     mail,
		AltAuth:  altAuth,
		Tracer:   tp.Tracer("altalune.id/yasaku"),
		Meter:    mp.Meter("altalune.id/yasaku"),
		Notify:   sinks,
		Nano:     nanoid.New,
		Caps:     caps,
		Outbox:   outbox.NewStore(cfg.DB, pool, pgConn),
	}
	for _, s := range sinks {
		if c, ok := s.(io.Closer); ok {
			kernel.AddCloser(c)
		}
	}
	kernel.AddCloser(pool)

	q, err := openQueue(ctx, cfg, kernel, reporter, log)
	if err != nil {
		_ = pool.Close()
		_ = shutdownOTel(context.Background())
		return nil, fmt.Errorf("boot: queue: %w", err)
	}
	kernel.Queue = q
	kernel.AddCloser(q)
	abort := func() {
		_ = q.Close()
		_ = pool.Close()
		_ = shutdownOTel(context.Background())
	}

	startSeq, err := q.BroadcastStartSeq(ctx)
	if err != nil {
		abort()
		return nil, fmt.Errorf("boot: queue: %w", err)
	}

	required := &atomic.Bool{}

	svcs, err := buildServices(cfg, kernel, caps)
	if err != nil {
		abort()
		return nil, err
	}

	gate := &onboardingGate{required: required, queue: q, unexpected: reporter.Unexpected}
	hs, ls, err := declareQueue(q, svcs, gate, log)
	if err != nil {
		abort()
		return nil, err
	}

	onboarded, err := bootstrap(ctx, cfg, svcs.Users, svcs.Onboards, log)
	if err != nil {
		abort()
		return nil, fmt.Errorf("boot: bootstrap: %w", err)
	}
	caps.OnboardingRequired = !onboarded
	if !caps.LocalIdentity {
		has, hErr := svcs.Users.HasLocalUsers(ctx)
		if hErr != nil {
			log.Warn("boot: HasLocalUsers probe failed", slog.String("err", hErr.Error()))
		} else if has {
			caps.LocalIdentity = true
		}
	}
	kernel.Caps = caps

	health := db.NewHealthMonitor(pool, cfg.DB.Health, log, kernel.Meter)
	if pErr := health.Probe(ctx); pErr != nil {
		log.Warn("boot: initial db health probe failed", slog.String("err", pErr.Error()))
	}

	sup := worker.New(log)
	sup.Register(health)
	sup.Register(svcs.APIKeyUsage)
	sup.Register(outbox.NewWorker(kernel.Outbox, dispatchDeliverer(o, kernel, svcs, log), orgEnumerator(cfg, kernel, log), log, o.dispatch))
	if cfg.Telemetry.Metrics.Prometheus.Enabled {
		sup.Register(telemetry.PrometheusWorker(cfg.Telemetry.Metrics.Prometheus, log))
	}

	var runner *scheduler.Runner
	switch off := schedulerOff(o, cfg); off {
	case "":
		r, sErr := buildScheduler(cfg, kernel, svcs, log)
		if sErr != nil {
			abort()
			return nil, sErr
		}
		runner = r
		sup.Register(runner)
	default:
		log.Info("boot: scheduler disabled by " + off)
	}

	if o.schedulerOnly && runner == nil {
		abort()
		return nil, fmt.Errorf(
			"boot: --scheduler-only requires the scheduler, but the scheduler is disabled by %s", schedulerOff(o, cfg))
	}

	var consumer *queue.Consumer
	switch off := consumerOff(o, cfg); off {
	case "":
		c, cErr := queue.NewConsumer(ctx, q, hs)
		if cErr != nil {
			abort()
			return nil, fmt.Errorf("boot: queue consumer: %w", cErr)
		}
		consumer = c
		sup.Register(consumer)
	default:
		log.Info("boot: consumer disabled by " + off)
	}

	if o.consumerOnly && consumer == nil {
		abort()
		return nil, fmt.Errorf(
			"boot: --consumer-only requires the queue, but the queue is disabled by %s", consumerOff(o, cfg))
	}

	var listener *queue.Listen
	if cfg.Queue.Enabled {
		l, lErr := queue.NewListener(q, ls, startSeq)
		if lErr != nil {
			abort()
			return nil, fmt.Errorf("boot: queue listener: %w", lErr)
		}
		listener = l
		sup.Register(listener)
	}

	apiSrv, apiHandler := buildAPIHandler(cfg, kernel, svcs)
	dataHandler := buildDataHandler(cfg, caps, log, svcs)

	mcpSurf, err := buildMCPSurface(ctx, cfg, log, svcs, apiSrv)
	if err != nil {
		abort()
		return nil, err
	}
	mcpSurf = withRootMetadataAlias(mcpSurf)

	bundle, defaultLoc, err := buildI18nBundle(cfg)
	if err != nil {
		abort()
		return nil, err
	}

	healthOK := health.Ready

	required.Store(!onboarded)

	var setup string
	if required.Load() {
		t, tErr := setupToken(cfg)
		if tErr != nil {
			abort()
			return nil, fmt.Errorf("boot: setup token: %w", tErr)
		}
		setup = t
		logSetupToken(cfg, log, setup)
	}

	webHandler, webRoutes := buildWebHandler(webHandlerDeps{
		Cfg:         cfg,
		Kernel:      kernel,
		Caps:        caps,
		Log:         log,
		Reporter:    reporter,
		HealthOK:    healthOK,
		Services:    svcs,
		Required:    required,
		OnComplete:  gate.Complete,
		SetupToken:  setup,
		APIHandler:  apiHandler,
		DataHandler: dataHandler,
		MCP:         mcpSurf,
		Bundle:      bundle,
		DefaultLoc:  defaultLoc,
	})

	httpHandler := webHandler
	if o.schedulerOnly || o.consumerOnly {
		httpHandler = healthOnlyHandler(cfg, healthOK)
	}
	sup.Register(worker.HTTP("http", cfg.HTTP.Addr, httpHandler, log))

	return &Server{
		Cfg:                cfg,
		Caps:               caps,
		Platform:           kernel,
		Auth:               svcs.Auth,
		Users:              svcs.Users,
		Orgs:               svcs.Orgs,
		Projects:           svcs.Projects,
		Todos:              svcs.Todos,
		Invites:            svcs.Invites,
		Onboards:           svcs.Onboards,
		Posts:              svcs.Posts,
		Categories:         svcs.Categories,
		Tags:               svcs.Tags,
		APIKeys:            svcs.APIKeys,
		Webhooks:           svcs.Webhooks,
		Onboarded:          onboarded,
		Routes:             webRoutes,
		SetupToken:         setup,
		Onboard:            svcs.Onboard,
		Web:                webHandler,
		API:                apiSrv,
		MCP:                mcpSurf.Server,
		Scheduler:          runner,
		Consumer:           consumer,
		Listener:           listener,
		CompleteOnboarding: gate.Complete,
		httpHandler:        httpHandler,
		Health:             health,
		Supervisor:         sup,
		shutdownOTel:       shutdownOTel,
	}, nil
}

// Run starts every registered worker and blocks until ctx is canceled or a worker fails.
func (s *Server) Run(ctx context.Context) error {
	return s.Supervisor.Run(ctx)
}

// Close shuts OTel down and then releases every platform resource.
func (s *Server) Close() error {
	var errs []error
	if s.shutdownOTel != nil {
		if err := s.shutdownOTel(context.Background()); err != nil {
			errs = append(errs, err)
		}
	}
	if s.Platform != nil {
		if err := s.Platform.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func dispatchDeliverer(o *options, k *platform.Kernel, svcs *Services, log *slog.Logger) outbox.Deliverer {
	if o.deliverer != nil {
		return o.deliverer
	}
	return webhook.NewDeliverer(svcs.WebhookStore, k.Sealer, webhookHTTPClient(), log)
}

// SECURITY: otel stays off — otelhttp records url.full including the query string, and webhook.Deliver's own span is the only trace a delivery should leave.
func webhookHTTPClient() *http.Client {
	return httpclient.New(
		httpclient.WithTimeout(webhookTimeout),
		httpclient.WithResponseBodyLimit(webhookResponseLimit),
		httpclient.WithOtel(false),
	)
}

func setupToken(cfg *config.Config) (string, error) {
	if t := strings.TrimSpace(cfg.Onboard.SetupToken); t != "" {
		return t, nil
	}
	return nanoid.New(setupTokenLen)
}

func logSetupToken(cfg *config.Config, log *slog.Logger, token string) {
	url := onboardURL(cfg)
	if strings.TrimSpace(cfg.Onboard.SetupToken) != "" {
		log.Info("boot: setup required — /onboard is gated by the configured onboard.setupToken",
			slog.String("url", url))
		return
	}
	// SECURITY: the token rides in the url value because logger.Redact masks any attr key matching /token/.
	log.Info("boot: setup required — open this one-time onboarding URL",
		slog.String("url", url+"?token="+token),
	)
}

func onboardURL(cfg *config.Config) string {
	return strings.TrimRight(cfg.HTTP.BaseURL, "/") + cfg.HTTP.BasePath + "/onboard"
}

// NOTE: path must match "GET /oauth/callback" in internal/web/handlers/auth.go.
func oidcRedirectURL(cfg *config.Config) string {
	if cfg.HTTP.BaseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s%s/oauth/callback",
		strings.TrimRight(cfg.HTTP.BaseURL, "/"), cfg.HTTP.BasePath)
}

func buildAltAuth(ctx context.Context, cfg *config.Config, log *slog.Logger) (*authl.Client, error) {
	if cfg.OIDC.Issuer == "" {
		return nil, nil
	}
	secret, err := resolveStateSecret(cfg, log)
	if err != nil {
		return nil, err
	}
	redirect := oidcRedirectURL(cfg)
	return authl.NewClient(ctx, authl.Config{
		Issuer:           cfg.OIDC.Issuer,
		ClientID:         cfg.OIDC.ClientID,
		ClientSecret:     cfg.OIDC.ClientSecret,
		RedirectURL:      redirect,
		Scopes:           cfg.OIDC.Scopes,
		Resource:         cfg.OIDC.Resource,
		RememberLastUser: true,
		LastUserCookie:   "yasaku_last_user",
		StateCookie:      "yasaku_oidc_state",
		StateSecret:      secret,
		CookieSecure:     cfg.HTTP.CookieSecure,
	})
}

func resolveStateSecret(cfg *config.Config, log *slog.Logger) ([]byte, error) {
	raw := cfg.HTTP.StateSecret
	if raw != "" {
		buf, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			if b2, err2 := base64.StdEncoding.DecodeString(raw); err2 == nil {
				buf = b2
			} else {
				return nil, fmt.Errorf("config: http.stateSecret must be base64url — %w", err)
			}
		}
		if len(buf) < 32 {
			return nil, errors.New("config: http.stateSecret must decode to >= 32 bytes")
		}
		return buf, nil
	}
	ephemeral, err := authl.GenerateStateSecret()
	if err != nil {
		return nil, fmt.Errorf("mint ephemeral state secret: %w", err)
	}
	log.Warn("http.stateSecret is empty — using an ephemeral secret; set YASAKU_HTTP_STATE_SECRET to persist")
	return ephemeral, nil
}

func mailerConfig(m config.MailConfig) mailer.Config {
	return mailer.Config{
		Driver: m.Driver,
		From:   m.From,
		SMTP: mailer.SMTPConfig{
			Host: m.SMTP.Host,
			Port: m.SMTP.Port,
			User: m.SMTP.User,
			Pass: m.SMTP.Pass,
			TLS:  m.SMTP.TLS,
		},
		Resend: mailer.ResendConfig{
			APIKey:      m.Resend.APIKey,
			Endpoint:    m.Resend.Endpoint,
			MaxAttempts: m.Resend.MaxAttempts,
		},
	}
}

func openQueue(ctx context.Context, cfg *config.Config, k *platform.Kernel, reporter *apperror.Reporter, log *slog.Logger) (*queue.Client, error) {
	if !cfg.Queue.Enabled {
		return queue.Disabled(log), nil
	}
	return queue.Connect(ctx, queue.Options{
		URL:            cfg.Queue.URL,
		Token:          cfg.Queue.Token,
		ConnectTimeout: cfg.Queue.ConnectTimeout,
		Log:            log,
		Tracer:         k.Tracer,
		Meter:          k.Meter,
		Unexpected:     reporter.Unexpected,
	})
}

func declareQueue(q *queue.Client, svcs *Services, gate *onboardingGate, log *slog.Logger) ([]queue.Handler, []queue.Listener, error) {
	providers := consumerProviders(svcs, log)
	if err := assertConsumerWiring(providers); err != nil {
		return nil, nil, err
	}
	hs := handlersOf(publishedConsumers(providers))
	ls := listenersOf(listenerProviders(gate))
	if err := q.Declare(jobsOf(hs), broadcastsOf(ls)); err != nil {
		return nil, nil, fmt.Errorf("boot: queue declare: %w", err)
	}
	return hs, ls, nil
}

func schedulerOff(o *options, cfg *config.Config) string {
	switch {
	case !o.scheduler:
		return "WithScheduler(false)"
	case o.consumerOnly:
		return "--consumer-only"
	case !cfg.Scheduler.Enabled:
		return "scheduler.enabled=false"
	}
	return ""
}

func consumerOff(o *options, cfg *config.Config) string {
	switch {
	case !o.consumer:
		return "WithConsumer(false)"
	case o.schedulerOnly:
		return "--scheduler-only"
	case !cfg.Queue.Enabled:
		return "queue.enabled=false"
	}
	return ""
}
