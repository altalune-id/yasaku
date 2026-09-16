package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type testErr struct{ code string }

func (e *testErr) Error() string { return "test error: " + e.code }

func staticScopes(scopes ...string) Option {
	return WithScopes(func(context.Context) []string { return scopes })
}

func testMapper(_ context.Context, err error) (ErrorPayload, bool) {
	var fs *ForbiddenScopeError
	if errors.As(err, &fs) {
		return ErrorPayload{Code: "MCP002", Message: "missing scope " + string(fs.Need)}, true
	}
	var te *testErr
	if errors.As(err, &te) {
		return ErrorPayload{Code: te.code, Message: "mapped failure"}, true
	}
	return ErrorPayload{}, false
}

func echoHandler(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	out, err := json.Marshal(map[string]any{"echo": input})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func failHandler(err error) Handler {
	return func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, err }
}

func readSpec() ToolSpec {
	return ToolSpec{
		Name:        "wallet_list",
		Description: "list wallets",
		Scope:       ScopeRead,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func writeSpec() ToolSpec {
	return ToolSpec{
		Name:        "wallet_create",
		Description: "create a wallet",
		Scope:       ScopeWrite,
		Mutation:    true,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func connect(t *testing.T, s *Server) *sdk.ClientSession {
	t.Helper()
	ctx := t.Context()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	ss, err := s.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func resultText(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("want exactly 1 content block, got %d", len(res.Content))
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("want *sdk.TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func errorPayload(t *testing.T, res *sdk.CallToolResult) (code, message string) {
	t.Helper()
	payload := decodeErrorPayload(t, res)
	return payload.Code, payload.Message
}

func decodeErrorPayload(t *testing.T, res *sdk.CallToolResult) ErrorPayload {
	t.Helper()
	if !res.IsError {
		t.Fatalf("want IsError result, got success: %s", resultText(t, res))
	}
	var payload ErrorPayload
	if err := json.Unmarshal([]byte(resultText(t, res)), &payload); err != nil {
		t.Fatalf("decoding error payload: %v", err)
	}
	return payload
}

func TestServerReadToolSucceeds(t *testing.T) {
	s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"), WithErrorMapper(testMapper))
	s.Register(readSpec(), echoHandler)

	res := callTool(t, connect(t, s), "wallet_list", map[string]any{"limit": 10})
	if res.IsError {
		t.Fatalf("want success, got error result: %s", resultText(t, res))
	}
	if got := resultText(t, res); !strings.Contains(got, `"limit":10`) {
		t.Fatalf("text content %q does not carry the handler output", got)
	}
	structured, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("want map StructuredContent, got %T", res.StructuredContent)
	}
	if _, ok := structured["echo"]; !ok {
		t.Fatalf("StructuredContent %v missing echo key", structured)
	}
}

func TestServerScopeEnforcement(t *testing.T) {
	tests := []struct {
		name      string
		scopes    []string
		tool      string
		wantError bool
		wantNeed  Scope
	}{
		{name: "read scope calls read tool", scopes: []string{"yasaku:read"}, tool: "wallet_list"},
		{name: "read scope refused on write tool", scopes: []string{"yasaku:read"}, tool: "wallet_create", wantError: true, wantNeed: ScopeWrite},
		{name: "write scope alone calls write tool", scopes: []string{"yasaku:write"}, tool: "wallet_create"},
		{name: "both scopes call write tool", scopes: []string{"yasaku:read", "yasaku:write"}, tool: "wallet_create"},
		{name: "write scope alone refused on read tool", scopes: []string{"yasaku:write"}, tool: "wallet_list", wantError: true, wantNeed: ScopeRead},
		{name: "no scopes refused", scopes: nil, tool: "wallet_list", wantError: true, wantNeed: ScopeRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer("yasaku", "1.0.0", staticScopes(tt.scopes...), WithErrorMapper(testMapper))
			s.Register(readSpec(), echoHandler)
			s.Register(writeSpec(), echoHandler)

			res := callTool(t, connect(t, s), tt.tool, map[string]any{})
			if !tt.wantError {
				if res.IsError {
					t.Fatalf("want success, got error result: %s", resultText(t, res))
				}
				return
			}
			code, message := errorPayload(t, res)
			if code != "MCP002" {
				t.Fatalf("code = %q, want MCP002", code)
			}
			if !strings.Contains(message, string(tt.wantNeed)) {
				t.Fatalf("message %q does not name the missing scope %q", message, tt.wantNeed)
			}
		})
	}
}

func TestServerScopeRefusalWithoutMapperIsGeneric(t *testing.T) {
	var buf bytes.Buffer
	s := NewServer("yasaku", "1.0.0",
		staticScopes(),
		WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
	)
	s.Register(readSpec(), echoHandler)

	code, message := errorPayload(t, callTool(t, connect(t, s), "wallet_list", map[string]any{}))
	if code != CodeUnexpected || message != "unexpected error" {
		t.Fatalf("got (%q, %q), want (%q, %q)", code, message, CodeUnexpected, "unexpected error")
	}
	if !strings.Contains(buf.String(), "yasaku:read") {
		t.Fatalf("log %q does not carry the refusal cause", buf.String())
	}
}

func TestServerMappedHandlerErrorUsesMapperCode(t *testing.T) {
	s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"), WithErrorMapper(testMapper))
	s.Register(readSpec(), failHandler(&testErr{code: "WAL404"}))

	code, message := errorPayload(t, callTool(t, connect(t, s), "wallet_list", map[string]any{}))
	if code != "WAL404" || message != "mapped failure" {
		t.Fatalf("got (%q, %q), want (WAL404, mapped failure)", code, message)
	}
}

func TestServerUnmappedHandlerErrorIsGEN900AndLogged(t *testing.T) {
	var buf bytes.Buffer
	s := NewServer("yasaku", "1.0.0",
		staticScopes("yasaku:read"),
		WithErrorMapper(testMapper),
		WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
	)
	s.Register(readSpec(), failHandler(errors.New("boom")))

	code, message := errorPayload(t, callTool(t, connect(t, s), "wallet_list", map[string]any{}))
	if code != "GEN900" || message != "unexpected error" {
		t.Fatalf("got (%q, %q), want (GEN900, unexpected error)", code, message)
	}
	logged := buf.String()
	if !strings.Contains(logged, "boom") {
		t.Fatalf("log %q does not carry the real cause", logged)
	}
	if !strings.Contains(logged, "wallet_list") {
		t.Fatalf("log %q does not name the tool", logged)
	}
}

func TestServerNilArgumentsArriveAsEmptyObject(t *testing.T) {
	t.Run("normalizeInput", func(t *testing.T) {
		tests := []struct {
			name string
			in   json.RawMessage
		}{
			{name: "nil", in: nil},
			{name: "empty", in: json.RawMessage{}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := string(normalizeInput(tt.in)); got != "{}" {
					t.Fatalf("normalizeInput = %q, want {}", got)
				}
			})
		}
	})

	t.Run("over the wire", func(t *testing.T) {
		var seen json.RawMessage
		s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"), WithErrorMapper(testMapper))
		s.Register(readSpec(), func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
			seen = input
			return json.RawMessage(`{"ok":true}`), nil
		})

		res := callTool(t, connect(t, s), "wallet_list", nil)
		if res.IsError {
			t.Fatalf("want success, got error result: %s", resultText(t, res))
		}
		if string(seen) != "{}" {
			t.Fatalf("handler input = %q, want {}", string(seen))
		}
	})
}

func TestServerNonObjectHandlerOutputIsGEN900(t *testing.T) {
	tests := []struct {
		name string
		out  string
	}{
		{name: "not json", out: `not json`},
		{name: "array", out: `[1,2,3]`},
		{name: "string", out: `"ok"`},
		{name: "null", out: `null`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := NewServer("yasaku", "1.0.0",
				staticScopes("yasaku:read"),
				WithErrorMapper(testMapper),
				WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
			)
			s.Register(readSpec(), func(context.Context, json.RawMessage) (json.RawMessage, error) {
				return json.RawMessage(tt.out), nil
			})

			res := callTool(t, connect(t, s), "wallet_list", map[string]any{})
			code, _ := errorPayload(t, res)
			if code != CodeUnexpected {
				t.Fatalf("code = %q, want %q", code, CodeUnexpected)
			}
			if res.StructuredContent != nil {
				t.Fatalf("StructuredContent = %v, want nil on a rejected result", res.StructuredContent)
			}
			if buf.Len() == 0 {
				t.Fatal("want the rejected result logged")
			}
		})
	}
}

func TestServerAnnotationsReflectMutation(t *testing.T) {
	s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"))
	s.Register(readSpec(), echoHandler)
	s.Register(writeSpec(), echoHandler)

	res, err := connect(t, s).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(res.Tools))
	}
	for _, tool := range res.Tools {
		if tool.Annotations == nil {
			t.Fatalf("tool %s has no annotations", tool.Name)
		}
		mutation := tool.Name == "wallet_create"
		if tool.Annotations.ReadOnlyHint == mutation {
			t.Fatalf("tool %s ReadOnlyHint = %v, want %v", tool.Name, tool.Annotations.ReadOnlyHint, !mutation)
		}
		if tool.Annotations.DestructiveHint == nil {
			t.Fatalf("tool %s has no DestructiveHint", tool.Name)
		}
		if *tool.Annotations.DestructiveHint != mutation {
			t.Fatalf("tool %s DestructiveHint = %v, want %v", tool.Name, *tool.Annotations.DestructiveHint, mutation)
		}
		if tool.Description == "" {
			t.Fatalf("tool %s lost its description", tool.Name)
		}
	}
}

func TestRegisterRejectsInvalidSpec(t *testing.T) {
	tests := []struct {
		name string
		spec ToolSpec
		h    Handler
	}{
		{name: "empty name", spec: ToolSpec{Scope: ScopeRead}, h: echoHandler},
		{name: "unknown scope", spec: ToolSpec{Name: "x", Scope: Scope("yasaku:admin")}, h: echoHandler},
		{name: "empty scope", spec: ToolSpec{Name: "x"}, h: echoHandler},
		{name: "nil handler", spec: ToolSpec{Name: "x", Scope: ScopeRead}, h: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("want panic, got none")
				}
			}()
			NewServer("yasaku", "1.0.0").Register(tt.spec, tt.h)
		})
	}
}

func TestRegisterRejectsDuplicateToolName(t *testing.T) {
	s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"))
	s.Register(readSpec(), echoHandler)

	defer func() {
		if recover() == nil {
			t.Fatal("want panic on a duplicate tool name, got none")
		}
	}()
	second := readSpec()
	second.Description = "a different tool that reuses the name"
	s.Register(second, echoHandler)
}

func TestSDKLogsReachInjectedLogger(t *testing.T) {
	var buf bytes.Buffer
	s := NewServer("yasaku", "1.0.0",
		staticScopes("yasaku:read"),
		WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
	)
	spec := readSpec()
	spec.Name = "wallet list"
	s.Register(spec, echoHandler)

	logged := buf.String()
	if !strings.Contains(logged, "invalid tool name") {
		t.Fatalf("log %q does not carry the SDK name validation failure", logged)
	}
}

func TestOptionsIgnoreNilArguments(t *testing.T) {
	s := NewServer("yasaku", "1.0.0",
		staticScopes("yasaku:read"),
		WithErrorMapper(testMapper),
		WithScopes(nil),
		WithErrorMapper(nil),
		WithLogger(nil),
	)
	s.Register(readSpec(), echoHandler)
	s.Register(writeSpec(), failHandler(&testErr{code: "WAL500"}))

	cs := connect(t, s)
	if res := callTool(t, cs, "wallet_list", map[string]any{}); res.IsError {
		t.Fatalf("WithScopes(nil) dropped the configured reader: %s", resultText(t, res))
	}
	res, err := cs.CallTool(t.Context(), &sdk.CallToolParams{Name: "wallet_create", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	code, _ := errorPayload(t, res)
	if code != "MCP002" {
		t.Fatalf("code = %q, want MCP002 — WithErrorMapper(nil) dropped the configured mapper", code)
	}
}

func TestZeroServerIsUnusable(t *testing.T) {
	tests := []struct {
		name string
		call func(*Server)
	}{
		{name: "Register", call: func(s *Server) { s.Register(readSpec(), echoHandler) }},
		{name: "Handler", call: func(s *Server) { _ = s.Handler() }},
		{name: "SDK", call: func(s *Server) { _ = s.SDK() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("want panic on zero-value Server, got none")
				}
			}()
			tt.call(&Server{})
		})
	}
}

func TestServerImplementsRegistry(t *testing.T) {
	var reg Registry = NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"))
	reg.Register(readSpec(), echoHandler)
}

func TestHandlerServesPostOnly(t *testing.T) {
	s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"))
	s.Register(readSpec(), echoHandler)

	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", res.StatusCode)
	}

	getReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new GET request: %v", err)
	}
	getRes, err := srv.Client().Do(getReq)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = getRes.Body.Close() }()
	if getRes.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", getRes.StatusCode)
	}
}

func TestIsForbiddenScopeError(t *testing.T) {
	err := &ForbiddenScopeError{Tool: "wallet_create", Need: ScopeWrite}
	if !IsForbiddenScopeError(err) {
		t.Fatal("IsForbiddenScopeError(*ForbiddenScopeError) = false")
	}
	if !IsForbiddenScopeError(errors.Join(errors.New("ctx"), err)) {
		t.Fatal("IsForbiddenScopeError does not unwrap")
	}
	if IsForbiddenScopeError(errors.New("boom")) {
		t.Fatal("IsForbiddenScopeError(other) = true")
	}
	if IsForbiddenScopeError(nil) {
		t.Fatal("IsForbiddenScopeError(nil) = true")
	}
	if !strings.Contains(err.Error(), "wallet_create") || !strings.Contains(err.Error(), "yasaku:write") {
		t.Fatalf("Error() = %q, want tool and scope named", err.Error())
	}
}

func TestErrorPayloadCarriesCanonicalFields(t *testing.T) {
	full := ErrorPayload{
		Code:      "WAL404",
		Message:   `wallet "01H8" not found`,
		Meta:      map[string]string{"wallet_id": "01H8"},
		RequestID: "V1StGXR8",
		TraceID:   "4bf92f3577b34da6a3ce929d0e0e4736",
	}

	t.Run("full payload reaches the client intact", func(t *testing.T) {
		s := NewServer("yasaku", "1.0.0",
			staticScopes("yasaku:read"),
			WithErrorMapper(func(context.Context, error) (ErrorPayload, bool) { return full, true }),
		)
		s.Register(readSpec(), failHandler(errors.New("boom")))

		got := decodeErrorPayload(t, callTool(t, connect(t, s), "wallet_list", map[string]any{}))
		if got.Code != full.Code || got.Message != full.Message {
			t.Fatalf("got (%q, %q), want (%q, %q)", got.Code, got.Message, full.Code, full.Message)
		}
		if got.RequestID != full.RequestID {
			t.Fatalf("RequestID = %q, want %q", got.RequestID, full.RequestID)
		}
		if got.TraceID != full.TraceID {
			t.Fatalf("TraceID = %q, want %q", got.TraceID, full.TraceID)
		}
		if got.Meta["wallet_id"] != "01H8" {
			t.Fatalf("Meta = %v, want wallet_id 01H8", got.Meta)
		}
	})

	t.Run("minimal payload is exactly code and message", func(t *testing.T) {
		s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"), WithErrorMapper(testMapper))
		s.Register(readSpec(), failHandler(&testErr{code: "WAL404"}))

		got := resultText(t, callTool(t, connect(t, s), "wallet_list", map[string]any{}))
		if want := `{"code":"WAL404","message":"mapped failure"}`; got != want {
			t.Fatalf("payload = %s, want %s", got, want)
		}
	})

	t.Run("unmapped failure is exactly the generic payload", func(t *testing.T) {
		s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"), WithErrorMapper(testMapper))
		s.Register(readSpec(), failHandler(errors.New("boom")))

		got := resultText(t, callTool(t, connect(t, s), "wallet_list", map[string]any{}))
		if want := `{"code":"GEN900","message":"unexpected error"}`; got != want {
			t.Fatalf("payload = %s, want %s", got, want)
		}
	})
}

func TestMapperWithoutCodeFallsBackToGeneric(t *testing.T) {
	var buf bytes.Buffer
	s := NewServer("yasaku", "1.0.0",
		staticScopes("yasaku:read"),
		WithErrorMapper(func(context.Context, error) (ErrorPayload, bool) {
			return ErrorPayload{Message: "no code"}, true
		}),
		WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
	)
	s.Register(readSpec(), failHandler(errors.New("boom")))

	code, message := errorPayload(t, callTool(t, connect(t, s), "wallet_list", map[string]any{}))
	if code != CodeUnexpected || message != "unexpected error" {
		t.Fatalf("got (%q, %q), want (%q, unexpected error)", code, message, CodeUnexpected)
	}
	if !strings.Contains(buf.String(), "no code") {
		t.Fatalf("log %q does not report the mapper defect", buf.String())
	}
}

type reqIDKey struct{}

// reqIDHandler stands in for a production ctx-aware handler that stamps the request id on every record.
type reqIDHandler struct {
	slog.Handler
	mu   sync.Mutex
	seen []string
}

func (h *reqIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := ctx.Value(reqIDKey{}).(string); ok {
		h.mu.Lock()
		h.seen = append(h.seen, id)
		h.mu.Unlock()
	}
	return h.Handler.Handle(ctx, r)
}

func (h *reqIDHandler) requestIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.seen)
}

func httpSession(t *testing.T, s *Server, reqID string) *sdk.ClientSession {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), reqIDKey{}, reqID)
		s.Handler().ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestMapperReceivesRequestContext(t *testing.T) {
	s := NewServer("yasaku", "1.0.0",
		staticScopes("yasaku:read"),
		WithErrorMapper(func(ctx context.Context, _ error) (ErrorPayload, bool) {
			reqID, _ := ctx.Value(reqIDKey{}).(string)
			return ErrorPayload{Code: "WAL500", Message: "failed", RequestID: reqID}, true
		}),
	)
	s.Register(readSpec(), failHandler(errors.New("boom")))

	cs := httpSession(t, s, "V1StGXR8")
	res, err := cs.CallTool(t.Context(), &sdk.CallToolParams{Name: "wallet_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := decodeErrorPayload(t, res).RequestID; got != "V1StGXR8" {
		t.Fatalf("RequestID = %q, want V1StGXR8 — the mapper did not receive the HTTP request context", got)
	}
}

func TestDegradeLogsCarryRequestContext(t *testing.T) {
	tests := []struct {
		name string
		h    Handler
	}{
		{name: "unmapped failure", h: failHandler(errors.New("boom"))},
		{name: "non-object result", h: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`[1,2,3]`), nil
		}},
		{name: "null result", h: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`null`), nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			recorder := &reqIDHandler{Handler: slog.NewJSONHandler(&buf, nil)}
			s := NewServer("yasaku", "1.0.0", staticScopes("yasaku:read"), WithLogger(slog.New(recorder)))
			s.Register(readSpec(), tt.h)

			cs := httpSession(t, s, "V1StGXR8")
			res, err := cs.CallTool(t.Context(), &sdk.CallToolParams{Name: "wallet_list", Arguments: map[string]any{}})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			if code, _ := errorPayload(t, res); code != CodeUnexpected {
				t.Fatalf("code = %q, want %q", code, CodeUnexpected)
			}
			if !slices.Contains(recorder.requestIDs(), "V1StGXR8") {
				t.Fatalf("no degrade log carried the request context; log = %s", buf.String())
			}
		})
	}
}
