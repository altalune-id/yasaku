package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
	mcprt "altalune.id/yasaku/mcp"
)

const (
	testIssuer  = "https://idp.example"
	testSubject = "sub-42"
)

type stubVerifier struct {
	principal session.Principal
	err       error
}

var _ tokens.Verifier = stubVerifier{}

func (s stubVerifier) Verify(context.Context, string) (session.Principal, error) {
	if s.err != nil {
		return session.Principal{}, s.err
	}
	return s.principal, nil
}

type authFixture struct {
	server  *Server
	users   *fakes.User
	user    *user.User
	reached *session.Principal
	next    http.Handler
}

func newAuthFixture(t *testing.T, basePath string) *authFixture {
	t.Helper()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{BaseURL: "https://y.example", BasePath: basePath},
		MCP:  config.MCPConfig{Enabled: true, Audience: "https://y.example" + basePath + "/mcp"},
	}
	cfg.Tokens.Issuer = testIssuer

	users := fakes.NewUser()
	u := &user.User{
		ID:         uuid.New(),
		Email:      "owner@example.com",
		Name:       "Owner",
		Source:     user.SourceOIDC,
		IDPIssuer:  testIssuer,
		IDPSubject: testSubject,
	}
	if err := users.Save(t.Context(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	f := &authFixture{users: users, user: u}
	f.next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := session.PrincipalFrom(r.Context())
		f.reached = &p
		w.WriteHeader(http.StatusTeapot)
	})
	f.server = New(Deps{
		Cfg:      cfg,
		Verifier: stubVerifier{principal: session.Principal{IDPIssuer: testIssuer, IDPSubject: testSubject, Scopes: []string{"yasaku:read"}}},
		Users:    users,
		Version:  "test",
	})
	return f
}

func (f *authFixture) do(t *testing.T, header string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	f.server.auth(f.next).ServeHTTP(rec, req)
	return rec
}

func bodyCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func TestAuth_MissingHeaderChallenges(t *testing.T) {
	f := newAuthFixture(t, "")
	rec := f.do(t, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	want := `Bearer resource_metadata="https://y.example/.well-known/oauth-protected-resource/mcp"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
	if got := bodyCode(t, rec); got != apperror.CodeMCPUnauthenticated {
		t.Fatalf("body code = %q, want %q", got, apperror.CodeMCPUnauthenticated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if f.reached != nil {
		t.Fatal("the runtime must not be reached without a token")
	}
}

func TestAuth_ChallengeCarriesTheBasePath(t *testing.T) {
	f := newAuthFixture(t, "/app")
	rec := f.do(t, "")

	want := `Bearer resource_metadata="https://y.example/.well-known/oauth-protected-resource/app/mcp"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestAuth_WrongSchemeChallenges(t *testing.T) {
	for _, header := range []string{"Basic abc", "Bearer", "token abc", "Bearer "} {
		t.Run(header, func(t *testing.T) {
			f := newAuthFixture(t, "")
			rec := f.do(t, header)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := bodyCode(t, rec); got != apperror.CodeMCPUnauthenticated {
				t.Fatalf("body code = %q, want MCP001", got)
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("a challenge must accompany every 401")
			}
		})
	}
}

func TestAuth_BadTokenChallenges(t *testing.T) {
	f := newAuthFixture(t, "")
	f.server.Verifier = stubVerifier{err: &tokens.InvalidTokenError{Reason: "bad signature"}}

	rec := f.do(t, "Bearer nope")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := bodyCode(t, rec); got != apperror.CodeMCPUnauthenticated {
		t.Fatalf("body code = %q, want MCP001", got)
	}
	if rec.Body.String() == "" || f.reached != nil {
		t.Fatal("a rejected token must not reach the runtime")
	}
}

func TestAuth_UnknownSubjectIsForbidden(t *testing.T) {
	f := newAuthFixture(t, "")
	f.server.Verifier = stubVerifier{principal: session.Principal{IDPIssuer: testIssuer, IDPSubject: "stranger"}}

	rec := f.do(t, "Bearer ok")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if got := bodyCode(t, rec); got != apperror.CodeMCPUnknownUser {
		t.Fatalf("body code = %q, want %q", got, apperror.CodeMCPUnknownUser)
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatal("a 403 must not carry a bearer challenge")
	}
	if f.reached != nil {
		t.Fatal("an unknown subject must not reach the runtime")
	}
}

func TestAuth_StoreFailureIsUnexpected(t *testing.T) {
	f := newAuthFixture(t, "")
	f.users.ByIDPErr = errors.New("boom")
	f.users.StickyError = true

	rec := f.do(t, "Bearer ok")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := bodyCode(t, rec); got != mcprt.CodeUnexpected {
		t.Fatalf("body code = %q, want %q", got, mcprt.CodeUnexpected)
	}
}

func TestAuth_ValidTokenReachesTheRuntimeWithUserID(t *testing.T) {
	f := newAuthFixture(t, "")

	rec := f.do(t, "Bearer ok")
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the stub runtime's 418 (body %q)", rec.Code, rec.Body.String())
	}
	if f.reached == nil {
		t.Fatal("the runtime was not reached")
	}
	if f.reached.UserID != f.user.ID {
		t.Fatalf("principal.UserID = %v, want %v", f.reached.UserID, f.user.ID)
	}
	if f.reached.IDPSubject != testSubject {
		t.Fatalf("principal.IDPSubject = %q, want %q", f.reached.IDPSubject, testSubject)
	}
	if len(f.reached.Scopes) != 1 || f.reached.Scopes[0] != "yasaku:read" {
		t.Fatalf("principal.Scopes = %v, want [yasaku:read]", f.reached.Scopes)
	}
}

func TestAuth_CaseInsensitiveBearerScheme(t *testing.T) {
	f := newAuthFixture(t, "")
	if rec := f.do(t, "bearer ok"); rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the stub runtime's 418", rec.Code)
	}
}

func TestChallenge_SetsHeaderAndUnauthorizedBody(t *testing.T) {
	f := newAuthFixture(t, "")
	rec := httptest.NewRecorder()
	f.server.Challenge(rec)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("Challenge must set WWW-Authenticate")
	}
	if got := bodyCode(t, rec); got != apperror.CodeMCPUnauthenticated {
		t.Fatalf("body code = %q, want MCP001", got)
	}
}
