package boot

import (
	"context"
	"log/slog"

	"altalune.id/yasaku/internal/api"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/logger"
)

// Client is the CLI-side wired graph, omitting DB, migrations, and supervisor.
type Client struct {
	Cfg      *config.Config
	Log      *slog.Logger
	Reporter *apperror.Reporter
	Conn     *api.Client
}

// BootClient builds the minimal wired graph for CLI subcommands that talk to a remote yasaku server.
func BootClient(_ context.Context, cfg *config.Config, token string) (*Client, error) {
	log := logger.New(cfg.Log)
	reporter := apperror.NewReporter(log, cfg.Mode.IsProduction())
	conn := api.NewClient(cfg.HTTP.BaseURL, token)
	return &Client{
		Cfg:      cfg,
		Log:      log,
		Reporter: reporter,
		Conn:     conn,
	}, nil
}
