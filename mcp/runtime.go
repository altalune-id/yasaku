// Package mcp is a thin runtime over the MCP Go SDK that generated code registers tools against.
package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const unexpectedMessage = "unexpected error"

// Scope is an authorization scope a caller must hold to invoke a tool.
type Scope string

const (
	// ScopeRead is the scope read-only tools require.
	ScopeRead Scope = "yasaku:read"
	// ScopeWrite is the scope mutating tools require.
	ScopeWrite Scope = "yasaku:write"
)

// ToolSpec describes one MCP tool: its identity, the scope it needs, and its input schema.
type ToolSpec struct {
	Name        string
	Description string
	Scope       Scope
	Mutation    bool
	InputSchema json.RawMessage
}

// Handler executes one tool call over raw JSON in and raw JSON out.
type Handler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// Registry is the narrow surface generated code registers its tools against.
type Registry interface {
	Register(spec ToolSpec, h Handler)
}

// Option configures a Server at construction time.
type Option func(*Server)

// WithScopes sets the reader returning the scopes the calling identity holds.
func WithScopes(fn func(ctx context.Context) []string) Option {
	return func(s *Server) {
		if fn != nil {
			s.scopes = fn
		}
	}
}

// WithErrorMapper sets the mapper turning a handler error into the client-visible ErrorPayload.
func WithErrorMapper(fn func(ctx context.Context, err error) (ErrorPayload, bool)) Option {
	return func(s *Server) {
		if fn != nil {
			s.mapErr = fn
		}
	}
}

// WithLogger sets the logger for causes that are never shown to the caller.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) {
		if l != nil {
			s.logger = l
		}
	}
}

// Server hosts MCP tools over the SDK. Its zero value is unusable; build one with NewServer.
type Server struct {
	sdk *sdk.Server
	// NOTE: written only by Register, which is construction-time by contract; unguarded by design.
	names  map[string]struct{}
	scopes func(ctx context.Context) []string
	mapErr func(ctx context.Context, err error) (ErrorPayload, bool)
	logger *slog.Logger
}

// NewServer builds a Server that denies every scope, maps no error, and discards logs until options say otherwise.
func NewServer(name, version string, opts ...Option) *Server {
	s := &Server{
		names:  make(map[string]struct{}),
		scopes: func(context.Context) []string { return nil },
		mapErr: func(context.Context, error) (ErrorPayload, bool) { return ErrorPayload{}, false },
		logger: slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.sdk = sdk.NewServer(
		&sdk.Implementation{Name: name, Version: version},
		&sdk.ServerOptions{Logger: s.logger},
	)
	return s
}

// Register adds a tool to the server; it panics on a spec the generator should never produce.
func (s *Server) Register(spec ToolSpec, h Handler) {
	s.mustBuilt()
	if spec.Name == "" {
		panic("mcp: ToolSpec.Name must not be empty")
	}
	if spec.Scope != ScopeRead && spec.Scope != ScopeWrite {
		panic("mcp: ToolSpec.Scope must be ScopeRead or ScopeWrite, got " + string(spec.Scope))
	}
	if h == nil {
		panic("mcp: Handler must not be nil for tool " + spec.Name)
	}
	if _, dup := s.names[spec.Name]; dup {
		panic("mcp: duplicate tool name " + spec.Name)
	}
	s.names[spec.Name] = struct{}{}

	schema := spec.InputSchema
	if len(schema) == 0 {
		schema = json.RawMessage(`{"type":"object"}`)
	}
	mutation := spec.Mutation
	tool := &sdk.Tool{
		Name:        spec.Name,
		Description: spec.Description,
		InputSchema: schema,
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    !mutation,
			DestructiveHint: &mutation,
		},
	}

	s.sdk.AddTool(tool, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		if err := s.authorize(ctx, spec); err != nil {
			return s.failure(ctx, spec.Name, err), nil
		}
		out, err := h(ctx, normalizeInput(req.Params.Arguments))
		if err != nil {
			return s.failure(ctx, spec.Name, err), nil
		}
		return s.success(ctx, spec.Name, out), nil
	})
}

// Handler returns the stateless streamable HTTP handler; the endpoint is POST-only, as the SDK answers GET and DELETE with 405.
func (s *Server) Handler() http.Handler {
	s.mustBuilt()
	return sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return s.sdk },
		&sdk.StreamableHTTPOptions{Stateless: true, Logger: s.logger},
	)
}

// SDK exposes the underlying SDK server for transports the HTTP handler does not cover.
// NOTE: only Handler's stateless transport gives the scope reader and error mapper the live request context; a stateful session or stdio binds one context for the whole session, so request ids there are stale or absent.
func (s *Server) SDK() *sdk.Server {
	s.mustBuilt()
	return s.sdk
}

func (s *Server) mustBuilt() {
	if s.sdk == nil {
		panic("mcp: Server must be built with NewServer")
	}
}

func (s *Server) authorize(ctx context.Context, spec ToolSpec) error {
	if slices.Contains(s.scopes(ctx), string(spec.Scope)) {
		return nil
	}
	return &ForbiddenScopeError{Tool: spec.Name, Need: spec.Scope}
}

func (s *Server) success(ctx context.Context, tool string, out json.RawMessage) *sdk.CallToolResult {
	raw := normalizeInput(out)
	var structured map[string]any
	if err := json.Unmarshal(raw, &structured); err != nil {
		s.logger.ErrorContext(ctx, "mcp: tool returned JSON that is not an object", "tool", tool, "error", err)
		return unexpectedResult()
	}
	if structured == nil {
		s.logger.ErrorContext(ctx, "mcp: tool returned a null result", "tool", tool)
		return unexpectedResult()
	}
	return &sdk.CallToolResult{
		StructuredContent: structured,
		Content:           []sdk.Content{&sdk.TextContent{Text: string(raw)}},
	}
}

func (s *Server) failure(ctx context.Context, tool string, cause error) *sdk.CallToolResult {
	payload, ok := s.mapErr(ctx, cause)
	if ok && payload.Code == "" {
		s.logger.ErrorContext(ctx, "mcp: mapper returned a payload with no code", "tool", tool, "error", cause)
		ok = false
	}
	if !ok {
		s.logger.ErrorContext(ctx, "mcp: unmapped tool failure", "tool", tool, "error", cause)
		return unexpectedResult()
	}
	return errorResult(payload)
}

func unexpectedResult() *sdk.CallToolResult {
	return errorResult(ErrorPayload{Code: CodeUnexpected, Message: unexpectedMessage})
}

func errorResult(payload ErrorPayload) *sdk.CallToolResult {
	raw, _ := json.Marshal(payload)
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: string(raw)}},
	}
}

func normalizeInput(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}
