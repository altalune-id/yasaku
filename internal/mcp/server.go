// Package mcp mounts yasaku's MCP surface: bearer-token auth, the generated tool runtime and RFC 9728 metadata.
package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"go.opentelemetry.io/otel/trace"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/mcp/ui"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/user"
	mcprt "altalune.id/yasaku/mcp"
	"altalune.id/yasaku/reqid"
)

const serverName = "yasaku"

// Deps is everything the MCP surface is built from.
type Deps struct {
	Cfg      *config.Config
	Verifier tokens.Verifier
	Users    user.Store
	Log      *slog.Logger
	Version  string
	// Register receives the runtime registry so boot can install the generated tools.
	Register func(reg mcprt.Registry)
}

// Server is the MCP surface: an authenticated tool endpoint plus its protected-resource metadata.
type Server struct {
	// Verifier verifies the bearer token on every request; it is a field so a test can substitute a stub.
	Verifier tokens.Verifier

	users     user.Store
	log       *slog.Logger
	runtime   http.Handler
	challenge string
	metadata  []byte
	routes    []string
}

// New builds the MCP surface from cfg and installs the tools d.Register provides.
func New(d Deps) *Server {
	log := d.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	rt := mcprt.NewServer(serverName, d.Version,
		mcprt.WithScopes(scopesFrom),
		mcprt.WithErrorMapper(mapError),
		mcprt.WithLogger(log),
		mcprt.WithUI(d.Cfg != nil && d.Cfg.MCP.AppsUI),
	)
	if d.Register != nil {
		d.Register(rt)
	}
	appsUI := d.Cfg != nil && d.Cfg.MCP.AppsUI
	log.Info("mcp: apps ui", "enabled", appsUI, "resource", ui.ResourceURI)
	if appsUI {
		rt.AddUIResource(mcprt.UIResource{
			URI:  ui.ResourceURI,
			Name: "yasaku",
			Body: ui.Document(),
			Meta: map[string]any{"ui": map[string]any{"prefersBorder": true}},
		})
	}
	s := &Server{
		Verifier: d.Verifier,
		users:    d.Users,
		log:      log,
		runtime:  rt.Handler(),
	}
	s.initMetadata(d.Cfg)
	return s
}

// Handler returns the bearer-authenticated MCP endpoint.
func (s *Server) Handler() http.Handler { return s.auth(s.runtime) }

func scopesFrom(ctx context.Context) []string { return session.PrincipalFrom(ctx).Scopes }

// mapError turns a tool failure into the client-visible payload, leaving anything unrecognised to the runtime.
func mapError(ctx context.Context, err error) (mcprt.ErrorPayload, bool) {
	if err == nil {
		return mcprt.ErrorPayload{}, false
	}
	if ae, ok := apperror.AsAppError(err); ok {
		return withContext(ctx, mcprt.ErrorPayload{Code: ae.Code(), Message: ae.Message(), Meta: metaOf(ae)}), true
	}
	if ce, ok := errors.AsType[*connect.Error](err); ok {
		return withContext(ctx, mcprt.ErrorPayload{Code: ce.Code().String(), Message: ce.Message()}), true
	}
	if mcprt.IsForbiddenScopeError(err) {
		return withContext(ctx, mcprt.ErrorPayload{Code: apperror.CodeMCPForbiddenScope, Message: "insufficient scope"}), true
	}
	return mcprt.ErrorPayload{}, false
}

func metaOf(ae *apperror.AppError) map[string]string {
	for _, d := range ae.Details() {
		if ed, ok := d.(*apperrorv1.ErrorDetail); ok && len(ed.GetMeta()) > 0 {
			return ed.GetMeta()
		}
	}
	return nil
}

func withContext(ctx context.Context, p mcprt.ErrorPayload) mcprt.ErrorPayload {
	p.RequestID = reqid.FromContext(ctx)
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		p.TraceID = sc.TraceID().String()
	}
	return p
}
