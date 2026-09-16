package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/config"
	mcprt "altalune.id/yasaku/mcp"
	"altalune.id/yasaku/reqid"
)

func newMetadataServer(t *testing.T, basePath string) *Server {
	t.Helper()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{BaseURL: "https://y.example", BasePath: basePath},
		MCP:  config.MCPConfig{Enabled: true, Audience: "https://y.example" + basePath + "/mcp"},
	}
	cfg.Tokens.Issuer = "https://idp.example"
	return New(Deps{Cfg: cfg, Verifier: stubVerifier{}, Users: nil, Version: "test"})
}

func TestMetadata_BodyAndHeaders(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		want     string
		routes   []string
	}{
		{
			name: "no base path",
			want: `{"resource":"https://y.example/mcp","authorization_servers":["https://idp.example"],` +
				`"scopes_supported":["yasaku:read","yasaku:write"],"bearer_methods_supported":["header"]}`,
			routes: []string{
				"/.well-known/oauth-protected-resource",
				"/.well-known/oauth-protected-resource/mcp",
			},
		},
		{
			name:     "base path",
			basePath: "/app",
			want: `{"resource":"https://y.example/app/mcp","authorization_servers":["https://idp.example"],` +
				`"scopes_supported":["yasaku:read","yasaku:write"],"bearer_methods_supported":["header"]}`,
			routes: []string{
				"/.well-known/oauth-protected-resource",
				"/.well-known/oauth-protected-resource/app/mcp",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newMetadataServer(t, tc.basePath)

			mux := http.NewServeMux()
			routes := s.WellKnown()
			patterns := make([]string, 0, len(routes))
			for pattern, h := range routes {
				patterns = append(patterns, pattern)
				mux.Handle(pattern, h)
			}
			slices.Sort(patterns)
			want := []string{"GET " + tc.routes[0], "GET " + tc.routes[1]}
			slices.Sort(want)
			if !slices.Equal(patterns, want) {
				t.Fatalf("WellKnown() patterns = %v, want %v", patterns, want)
			}

			for _, path := range tc.routes {
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
				}
				if got := rec.Body.String(); got != tc.want {
					t.Fatalf("GET %s body =\n%s\nwant\n%s", path, got, tc.want)
				}
				if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
					t.Fatalf("GET %s Content-Type = %q", path, ct)
				}
				if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
					t.Fatalf("GET %s Cache-Control = %q", path, cc)
				}
			}
		})
	}
}

func TestMetadata_HandlerServesDirectly(t *testing.T) {
	s := newMetadataServer(t, "")
	rec := httptest.NewRecorder()
	s.Metadata().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if doc["resource"] != "https://y.example/mcp" {
		t.Fatalf("resource = %v", doc["resource"])
	}
}

func TestHandlerIsPostOnlyThroughTheRuntime(t *testing.T) {
	s := newMetadataServer(t, "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — auth runs before the runtime's method check", rec.Code)
	}
}

func TestMapError(t *testing.T) {
	appErr := apperror.New("WAL001", "wallet gone", codes.NotFound,
		&apperrorv1.ErrorDetail{Code: "WAL001", Meta: map[string]string{"wallet": "cash"}})

	tests := []struct {
		name        string
		err         error
		wantOK      bool
		wantCode    string
		wantMessage string
		wantMeta    map[string]string
	}{
		{
			name:        "app error keeps code, message and meta",
			err:         appErr,
			wantOK:      true,
			wantCode:    "WAL001",
			wantMessage: "wallet gone",
			wantMeta:    map[string]string{"wallet": "cash"},
		},
		{
			name:        "connect error falls back to its code",
			err:         connect.NewError(connect.CodeInvalidArgument, errors.New("bad input")),
			wantOK:      true,
			wantCode:    connect.CodeInvalidArgument.String(),
			wantMessage: "bad input",
		},
		{
			name:        "forbidden scope maps to MCP002",
			err:         &mcprt.ForbiddenScopeError{Tool: "record_expense", Need: mcprt.ScopeWrite},
			wantOK:      true,
			wantCode:    apperror.CodeMCPForbiddenScope,
			wantMessage: "insufficient scope",
		},
		{
			name:   "unrecognised error is left to the runtime",
			err:    errors.New("boom"),
			wantOK: false,
		},
		{
			name:   "nil error is not claimed",
			err:    nil,
			wantOK: false,
		},
	}

	ctx := reqid.WithContext(t.Context(), "req-7")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mapError(ctx, tc.err)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (payload %+v)", ok, tc.wantOK, got)
			}
			if !tc.wantOK {
				return
			}
			if got.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", got.Code, tc.wantCode)
			}
			if got.Message != tc.wantMessage {
				t.Fatalf("message = %q, want %q", got.Message, tc.wantMessage)
			}
			if got.RequestID != "req-7" {
				t.Fatalf("request_id = %q, want req-7", got.RequestID)
			}
			for k, v := range tc.wantMeta {
				if got.Meta[k] != v {
					t.Fatalf("meta[%q] = %q, want %q", k, got.Meta[k], v)
				}
			}
		})
	}
}

func TestMapError_PrefersTheAppErrorInsideAConnectError(t *testing.T) {
	inner := apperror.New("PER003", "period closed", codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{Code: "PER003"})
	wrapped := connect.NewError(connect.CodeFailedPrecondition, inner)

	got, ok := mapError(t.Context(), wrapped)
	if !ok {
		t.Fatal("a connect error wrapping an AppError must be mapped")
	}
	if got.Code != "PER003" {
		t.Fatalf("code = %q, want PER003 — the domain code must survive the connect envelope", got.Code)
	}
}

func TestErrors_AppErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      interface{ ToAppError() *apperror.AppError }
		wantCode string
		wantGRPC codes.Code
	}{
		{"unauthenticated", &UnauthenticatedError{Reason: "missing token"}, apperror.CodeMCPUnauthenticated, codes.Unauthenticated},
		{"unknown user", &UnknownUserError{Subject: "sub-1"}, apperror.CodeMCPUnknownUser, codes.PermissionDenied},
		{"not member", &NotMemberError{}, apperror.CodeMCPNotMember, codes.PermissionDenied},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ae := tc.err.ToAppError()
			if ae.Code() != tc.wantCode {
				t.Fatalf("code = %q, want %q", ae.Code(), tc.wantCode)
			}
			if ae.GRPCCode() != tc.wantGRPC {
				t.Fatalf("grpc code = %v, want %v", ae.GRPCCode(), tc.wantGRPC)
			}
			if err, _ := tc.err.(error); err == nil || err.Error() == "" {
				t.Fatal("every typed error needs a message")
			}
		})
	}
}

func TestErrors_HelpersWalkWrappedChains(t *testing.T) {
	if !IsUnauthenticatedError(connect.NewError(connect.CodeUnauthenticated, &UnauthenticatedError{})) {
		t.Fatal("IsUnauthenticatedError must walk wrapped chains")
	}
	if !IsUnknownUserError(connect.NewError(connect.CodeUnauthenticated, &UnknownUserError{})) {
		t.Fatal("IsUnknownUserError must walk wrapped chains")
	}
	if !IsNotMemberError(connect.NewError(connect.CodeUnauthenticated, &NotMemberError{})) {
		t.Fatal("IsNotMemberError must walk wrapped chains")
	}
	if IsUnauthenticatedError(errors.New("nope")) || IsUnknownUserError(nil) || IsNotMemberError(nil) {
		t.Fatal("plain and nil errors must not match")
	}
}

func TestNew_RunsTheRegisterClosureOnce(t *testing.T) {
	cfg := &config.Config{
		HTTP: config.HTTPConfig{BaseURL: "https://y.example"},
		MCP:  config.MCPConfig{Enabled: true, Audience: "https://y.example/mcp"},
	}
	cfg.Tokens.Issuer = "https://idp.example"

	calls := 0
	New(Deps{
		Cfg:      cfg,
		Verifier: stubVerifier{},
		Version:  "test",
		Register: func(reg mcprt.Registry) {
			calls++
			reg.Register(
				mcprt.ToolSpec{Name: "probe", Scope: mcprt.ScopeRead},
				func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`{}`), nil },
			)
		},
	})
	if calls != 1 {
		t.Fatalf("Register closure ran %d times, want 1", calls)
	}
}
