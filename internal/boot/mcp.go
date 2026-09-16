package boot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/api"
	mcpsrv "altalune.id/yasaku/internal/mcp"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/user"
	mcprt "altalune.id/yasaku/mcp"
	"altalune.id/yasaku/version"
)

// mcpDomains names each slot in mcpRegistrars, in order.
//
//nolint:gochecknoglobals // Immutable wiring manifest; not runtime state.
var mcpDomains = []string{"workspace", "wallet", "category", "transaction", "period", "report"}

func mcpRegistrars(apiSrv *api.Server) []func(mcprt.Registry) {
	return []func(mcprt.Registry){
		func(reg mcprt.Registry) { yasakuv1mcp.RegisterWorkspaceServiceTools(reg, apiSrv.WorkspaceSvc) },
		func(reg mcprt.Registry) { yasakuv1mcp.RegisterWalletServiceTools(reg, apiSrv.WalletSvc) },
		func(reg mcprt.Registry) { yasakuv1mcp.RegisterCategoryServiceTools(reg, apiSrv.CategorySvc) },
		func(reg mcprt.Registry) { yasakuv1mcp.RegisterTransactionServiceTools(reg, apiSrv.TransactionSvc) },
		func(reg mcprt.Registry) { yasakuv1mcp.RegisterPeriodServiceTools(reg, apiSrv.PeriodSvc) },
		func(reg mcprt.Registry) { yasakuv1mcp.RegisterReportServiceTools(reg, apiSrv.ReportSvc) },
	}
}

func assertMCPWiring(rs []func(mcprt.Registry)) error {
	if len(rs) != len(mcpDomains) {
		return fmt.Errorf("mcp wiring: %d registrars for %d domains %v",
			len(rs), len(mcpDomains), mcpDomains)
	}
	missing := make([]string, 0, len(rs))
	for i, r := range rs {
		if r == nil {
			missing = append(missing, mcpDomains[i])
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("mcp wiring: domains missing a registrar: %v", missing)
	}
	return nil
}

// buildMCP wires the MCP endpoint and its protected-resource metadata, or nothing when mcp.enabled is false.
func buildMCP(
	ctx context.Context,
	cfg *config.Config,
	apiSrv *api.Server,
	users user.Store,
	log *slog.Logger,
	override tokens.Verifier,
) (http.Handler, map[string]http.Handler, error) {
	if !cfg.MCP.Enabled {
		return nil, nil, nil
	}
	registrars := mcpRegistrars(apiSrv)
	if err := assertMCPWiring(registrars); err != nil {
		return nil, nil, err
	}

	verifier := override
	if verifier == nil {
		// NOTE: a second verifier — the API surface keeps tokens.audience, while MCP tokens must
		// name the MCP endpoint itself (RFC 8707), so the two audiences never coincide.
		v, err := tokens.NewVerifier(ctx, tokens.Config{
			Issuer:        cfg.Tokens.Issuer,
			JWKSURL:       cfg.Tokens.JWKSURL,
			Audience:      cfg.MCP.Audience,
			JWKSCacheTTL:  cfg.Tokens.JWKSCacheTTL,
			ClockSkew:     cfg.Tokens.ClockSkew,
			SupportedAlgs: cfg.Tokens.SupportedAlgs,
			AcceptRS256:   cfg.Tokens.AcceptRS256,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("boot: mcp tokens: %w", err)
		}
		verifier = v
	}

	srv := mcpsrv.New(mcpsrv.Deps{
		Cfg:      cfg,
		Verifier: verifier,
		Users:    users,
		Log:      log,
		Version:  version.Default(),
		Register: func(reg mcprt.Registry) {
			for _, r := range registrars {
				r(reg)
			}
		},
	})
	log.Info("boot: mcp enabled",
		slog.String("endpoint", cfg.MCPEndpoint()),
		slog.String("audience", cfg.MCP.Audience),
		slog.Int("domains", len(mcpDomains)))
	return srv.Handler(), srv.WellKnown(), nil
}
