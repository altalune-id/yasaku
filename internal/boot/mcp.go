package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"

	"github.com/altalune-id/oidcrp"

	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/controlplane"
	mcpinternal "altalune.id/yasaku/internal/mcp"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/user"
	rootmcp "altalune.id/yasaku/mcp"
	"altalune.id/yasaku/version"
)

const mcpServerName = "yasaku"

type mcpSurface struct {
	Handler      http.Handler
	Metadata     http.Handler
	MetadataPath string
	Scopes       []string
	Server       *rootmcp.Server
}

// SECURITY: the token link is built on a second tokens.Verifier pinned to mcp.audience, never the kernel's control-plane verifier, so a token minted for one resource is refused at the other (RFC 8707).
func buildMCPSurface(cfg *config.Config, log *slog.Logger, s *Services, apiSrv *controlplane.Server, keys *oidcrp.KeySource) (mcpSurface, error) {
	if !cfg.MCP.Enabled {
		return mcpSurface{}, nil
	}
	resource, err := tokens.NewResource(cfg.MCP.Audience)
	if err != nil {
		return mcpSurface{}, fmt.Errorf("boot: mcp surface: %w", err)
	}
	verifier, err := tokens.NewVerifier(mcpTokensConfig(cfg), keys)
	if err != nil {
		return mcpSurface{}, fmt.Errorf("boot: mcp verifier: %w", err)
	}
	chain := authn.Chain{
		s.KeyAuthn,
		user.NewAuthenticator(
			tokens.NewAuthenticator(verifier, credentialScheme(s.KeyAuthn.Scheme(), cfg.MCP.Introspection)),
			s.UserStore,
			user.WithTenantResolution(
				orgStoreForOnboard{store: s.OrgStore},
				projectStoreForOnboard{store: s.ProjectStore},
			),
		),
	}

	registry := rootmcp.NewRegistry()
	if err := registerTools(registry, apiSrv); err != nil {
		return mcpSurface{}, fmt.Errorf("boot: mcp tools: %w", err)
	}

	opts := []rootmcp.Option{
		rootmcp.WithImplementation(mcpServerName, version.Default()),
		rootmcp.WithRegistry(registry),
		rootmcp.WithScopes(mcpinternal.ScopesFromContext),
		rootmcp.WithErrorMapper(mcpErrorPayload),
		rootmcp.WithLogger(log),
		rootmcp.WithUI(cfg.MCP.AppsUI),
	}
	srv := rootmcp.NewServer(opts...)
	if cfg.MCP.AppsUI {
		srv.AddUIResource(yasakuAppResource())
	}

	log.Info("boot: mcp surface mounted",
		slog.String("audience", cfg.MCP.Audience),
		slog.String("metadata", resource.MetadataPath),
		slog.Int("tools", len(registry.Names())),
		slog.Bool("ui", cfg.MCP.AppsUI))

	guard := mcpinternal.Authenticate(chain, resource, log)
	scopes := registeredScopes(registry)
	return mcpSurface{
		Handler:      guard(srv.Handler()),
		Metadata:     resource.MetadataHandler(cfg.Tokens.Issuer, scopes),
		MetadataPath: resource.MetadataPath,
		Scopes:       scopes,
		Server:       srv,
	}, nil
}

func mcpTokensConfig(cfg *config.Config) tokens.Config {
	c := cfg.Tokens
	c.Audience = cfg.MCP.Audience
	c.Introspection = cfg.MCP.Introspection
	if c.Introspection.MaxPerMinute == 0 {
		c.Introspection.MaxPerMinute = cfg.Tokens.Introspection.MaxPerMinute
	}
	return c
}

// SECURITY: the one scheme per resource; the API key prefix comes from the scheme that mints keys, so a dotted key never reads as a JWT.
func credentialScheme(keys apikey.Scheme, introspection tokens.IntrospectionConfig) authn.Scheme {
	scheme := keys.Authn()
	if introspection.Enabled() {
		scheme.TokenPrefix = tokens.PersonalTokenPrefix
	}
	return scheme
}

func registeredScopes(reg *rootmcp.Registry) []string {
	scopes := map[string]struct{}{}
	for _, name := range reg.Names() {
		spec, ok := reg.Spec(name)
		if !ok || spec.Scope == "" {
			continue
		}
		scopes[spec.Scope] = struct{}{}
	}
	return slices.Sorted(maps.Keys(scopes))
}

// SECURITY: anything TranslateError does not claim stays unmapped, so the root server logs the cause and answers a generic payload instead of handing the caller a raw Go error.
func mcpErrorPayload(ctx context.Context, err error) rootmcp.ErrorPayload {
	appErr, ok := apperror.AsAppError(mcpinternal.TranslateError(err))
	if !ok {
		return rootmcp.ErrorPayload{}
	}
	payload := rootmcp.ErrorPayload{Code: appErr.Code(), Message: appErr.Message()}
	if denied, ok := errors.AsType[*rootmcp.ScopeDeniedError](err); ok {
		payload.Meta = map[string]string{"tool": denied.Tool, "scope": denied.Scope}
	}
	if invalid, ok := errors.AsType[*rootmcp.InvalidArgumentsError](err); ok {
		payload.Meta = invalidArgumentsMeta(invalid)
	}
	return mcpinternal.AttachContext(ctx, payload)
}

func invalidArgumentsMeta(e *rootmcp.InvalidArgumentsError) map[string]string {
	meta := map[string]string{"tool": e.Tool}
	if e.Field != "" {
		meta["field"] = e.Field
	}
	return meta
}
