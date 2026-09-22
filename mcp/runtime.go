// Package mcp is a thin runtime over the MCP Go SDK that generated code registers tools against.
package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const unexpectedMessage = "unexpected error"

// MIMEApp is the MCP Apps media type for a single-file HTML UI resource.
const MIMEApp = "text/html;profile=mcp-app"

const (
	metaKeyUI = "ui"
)

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
	Name          string
	Description   string
	Scope         Scope
	Mutation      bool
	Destructive   bool
	InputSchema   json.RawMessage
	UIResourceURI string
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

// WithUI enables the MCP Apps link; when false, Register ignores ToolSpec.UIResourceURI.
func WithUI(enabled bool) Option {
	return func(s *Server) { s.ui = enabled }
}

// Server hosts MCP tools over the SDK. Its zero value is unusable; build one with NewServer.
type Server struct {
	sdk *sdk.Server
	// NOTE: names, uiRefs and resources are written only by Register and AddUIResource, which are construction-time by contract; unguarded by design.
	names     map[string]struct{}
	uiRefs    map[string]string
	resources map[string]UIResource
	ui        bool
	scopes    func(ctx context.Context) []string
	mapErr    func(ctx context.Context, err error) (ErrorPayload, bool)
	logger    *slog.Logger
}

// NewServer builds a Server that denies every scope, maps no error, and discards logs until options say otherwise.
func NewServer(name, version string, opts ...Option) *Server {
	s := &Server{
		names:     make(map[string]struct{}),
		uiRefs:    make(map[string]string),
		resources: make(map[string]UIResource),
		scopes:    func(context.Context) []string { return nil },
		mapErr:    func(context.Context, error) (ErrorPayload, bool) { return ErrorPayload{}, false },
		logger:    slog.New(slog.DiscardHandler),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.sdk = sdk.NewServer(
		&sdk.Implementation{Name: name, Version: version},
		&sdk.ServerOptions{Logger: s.logger},
	)
	s.sdk.AddReceivingMiddleware(s.trace)
	return s
}

// trace logs every inbound method, and what the client advertised at initialize.
func (s *Server) trace(next sdk.MethodHandler) sdk.MethodHandler {
	return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
		res, err := next(ctx, method, req)
		switch method {
		case "initialize":
			attrs := []any{"method", method}
			if p, ok := req.GetParams().(*sdk.InitializeParams); ok && p != nil {
				raw, _ := json.Marshal(p.Capabilities)
				attrs = append(attrs, "client", p.ClientInfo.Name, "client_version", p.ClientInfo.Version, "capabilities", string(raw))
			}
			s.logger.InfoContext(ctx, "mcp: request", attrs...)
		case "tools/call":
			// NOTE: the endpoint is one path, so an HTTP access log cannot say which tool ran; this is the only per-tool usage record.
			name := ""
			switch p := req.GetParams().(type) {
			case *sdk.CallToolParams:
				name = p.Name
			case *sdk.CallToolParamsRaw:
				name = p.Name
			}
			s.logger.InfoContext(ctx, "mcp: request", "method", method, "tool", name)
		default:
			s.logger.DebugContext(ctx, "mcp: request", "method", method)
		}
		return res, err
	}
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
	destructive := spec.Destructive
	closedWorld := false
	tool := &sdk.Tool{
		Name:        spec.Name,
		Title:       titleOf(spec.Name),
		Description: spec.Description,
		InputSchema: schema,
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    !mutation,
			DestructiveHint: &destructive,
			OpenWorldHint:   &closedWorld,
		},
	}

	if s.ui && spec.UIResourceURI != "" {
		// NOTE: the deprecated flat "ui/resourceUri" sibling is deliberately NOT emitted:
		// McpUiToolMeta sets additionalProperties:false, and a host validating the whole
		// _meta object rejects it. Hosts that render today read _meta.ui.resourceUri.
		tool.Meta = sdk.Meta{metaKeyUI: map[string]any{"resourceUri": spec.UIResourceURI}}
		s.uiRefs[spec.Name] = spec.UIResourceURI
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

// UIResource is one static MCP Apps bundle a tool's result can be rendered by.
type UIResource struct {
	URI      string
	Name     string
	MIMEType string
	Body     string
	Meta     map[string]any
}

// AddUIResource publishes r; it panics on a resource the app should never build.
func (s *Server) AddUIResource(r UIResource) {
	s.mustBuilt()
	if r.URI == "" {
		panic("mcp: UIResource.URI must not be empty")
	}
	if !strings.HasPrefix(r.URI, "ui://") {
		panic("mcp: UIResource.URI scheme must be ui, got " + r.URI)
	}
	if r.Body == "" {
		panic("mcp: UIResource.Body must not be empty for " + r.URI)
	}
	if _, dup := s.resources[r.URI]; dup {
		panic("mcp: duplicate UI resource " + r.URI)
	}
	if r.MIMEType == "" {
		r.MIMEType = MIMEApp
	}
	s.resources[r.URI] = r

	s.sdk.AddResource(&sdk.Resource{
		URI:      r.URI,
		Name:     r.Name,
		MIMEType: r.MIMEType,
	}, func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		return &sdk.ReadResourceResult{
			Contents: []*sdk.ResourceContents{{
				URI:      r.URI,
				MIMEType: r.MIMEType,
				Text:     r.Body,
				Meta:     sdk.Meta(r.Meta),
			}},
		}, nil
	})
}

// Handler returns the stateless streamable HTTP handler; the endpoint is POST-only, as the SDK answers GET and DELETE with 405. It panics if a tool references an unpublished UI resource.
func (s *Server) Handler() http.Handler {
	s.mustBuilt()
	s.mustResolved()
	return sdk.NewStreamableHTTPHandler(
		func(*http.Request) *sdk.Server { return s.sdk },
		&sdk.StreamableHTTPOptions{Stateless: true, Logger: s.logger},
	)
}

// SDK exposes the underlying SDK server for transports the HTTP handler does not cover. It panics if a tool references an unpublished UI resource.
// NOTE: only Handler's stateless transport gives the scope reader and error mapper the live request context; a stateful session or stdio binds one context for the whole session, so request ids there are stale or absent.
func (s *Server) SDK() *sdk.Server {
	s.mustBuilt()
	s.mustResolved()
	return s.sdk
}

// titleOf turns a snake_case tool name into the human-readable title hosts show in place of the name.
func titleOf(name string) string {
	t := strings.ReplaceAll(name, "_", " ")
	if t == "" {
		return t
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

func (s *Server) mustResolved() {
	for tool, uri := range s.uiRefs {
		if _, ok := s.resources[uri]; !ok {
			panic("mcp: tool " + tool + " references unpublished UI resource " + uri)
		}
	}
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
