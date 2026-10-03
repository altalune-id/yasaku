package boot_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	mcpv1 "altalune.id/yasaku/gen/go/mcp/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1mcp"
	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/onboard"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/platform/tokens"
	"altalune.id/yasaku/internal/user"
	rootmcp "altalune.id/yasaku/mcp"
	"altalune.id/yasaku/reqid"
)

const (
	mcpPath           = "/mcp"
	mcpAudience       = "http://127.0.0.1/mcp"
	controlAudience   = "http://127.0.0.1/api"
	tokenSubject      = "mcp-agent-subject"
	tokenEmail        = "mcp-agent@example.com"
	mcpMetadataPath   = "/.well-known/oauth-protected-resource/mcp"
	mcpProtocolHeader = "application/json, text/event-stream"

	walletCreateProcedure = "/api/yasaku.v1.WalletService/CreateWallet"
	walletListProcedure   = "/api/yasaku.v1.WalletService/ListWallets"
)

type tokenIssuer struct {
	url  string
	priv ed25519.PrivateKey
}

func newTokenIssuer(t *testing.T) *tokenIssuer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	iss := &tokenIssuer{priv: priv}
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	iss.url = ts.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer": "` + ts.URL + `",
			"authorization_endpoint": "` + ts.URL + `/authorize",
			"token_endpoint": "` + ts.URL + `/token",
			"jwks_uri": "` + ts.URL + `/jwks",
			"response_types_supported": ["code"],
			"subject_types_supported": ["public"],
			"id_token_signing_alg_values_supported": ["EdDSA"]
		}`))
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","alg":"EdDSA","use":"sig","kid":"k1","x":"` +
			base64.RawURLEncoding.EncodeToString(pub) + `"}]}`))
	})
	return iss
}

func (i *tokenIssuer) mint(t *testing.T, audience string, scopes []string) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss":   i.url,
		"sub":   tokenSubject,
		"aud":   audience,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
		"email": tokenEmail,
		"scope": strings.Join(scopes, " "),
	}
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": "k1"})
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sig := ed25519.Sign(i.priv, []byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

type mcpOpts struct {
	enabled        bool
	appsUI         bool
	challengeToken string
}

type mcpFixture struct {
	srv       *boot.Server
	issuer    *tokenIssuer
	log       *captureLog
	projectID string
	readKey   string
	noneKey   string
}

func newMCPFixture(t *testing.T, opts mcpOpts) *mcpFixture {
	t.Helper()

	issuer := newTokenIssuer(t)
	cfg := newSmokeCfg(t)
	cfg.Mode = config.ModeCloud
	cfg.OIDC = config.OIDCConfig{Issuer: stubIssuer(t), ClientID: "mcp-client", ClientSecret: "mcp-secret"}
	cfg.HTTP.CSP.Enabled = true
	cfg.API.Enabled = true
	cfg.API.KeyPrefix = apikey.DefaultPrefix
	cfg.Tokens = tokens.Config{Issuer: issuer.url, Audience: controlAudience}
	cfg.MCP = config.MCPConfig{
		Enabled: opts.enabled, AppsUI: opts.appsUI, Audience: mcpAudience,
		ChallengeToken: opts.challengeToken,
	}

	seed, err := boot.BootServer(context.Background(), cfg)
	require.NoError(t, err)
	seedUser, err := seed.Users.Create(context.Background(), user.CreateRequest{
		Email: "mcp-seed@example.com", Name: "MCP Seed", Source: user.SourceOIDC,
	})
	require.NoError(t, err)
	_, err = seed.Onboards.Complete(context.Background(), seedUser.ID, onboard.MethodCLIInit)
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	capture := &captureLog{}
	srv, err := boot.BootServer(context.Background(), cfg, boot.WithLogger(slog.New(capture)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })
	require.True(t, srv.Onboarded, "the fixture must be past the onboarding gate")

	ctx := context.Background()
	owner, err := srv.Users.EnsureFromOIDC(ctx, user.Claims{
		Issuer: issuer.url, Subject: tokenSubject, Email: tokenEmail, Name: "MCP Agent",
	})
	require.NoError(t, err)
	o, err := srv.Orgs.Create(ctx, org.CreateRequest{Slug: "mcp-org", Name: "MCP Org", OwnerID: owner.ID})
	require.NoError(t, err)

	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: owner.ID})
	p, err := srv.Projects.Create(orgCtx, o.ID, "mcp-project", "MCP Project")
	require.NoError(t, err)

	projCtx := tenant.WithProject(orgCtx, p.ID)
	_, readKey, err := srv.APIKeys.Mint(projCtx, "mcp-reader", []string{authn.ScopeYasakuRead}, nil, soon())
	require.NoError(t, err)
	_, noneKey, err := srv.APIKeys.Mint(projCtx, "mcp-keys-only", []string{authn.ScopeAPIKeysRead}, nil, soon())
	require.NoError(t, err)

	return &mcpFixture{
		srv:       srv,
		issuer:    issuer,
		log:       capture,
		projectID: p.ID.String(),
		readKey:   readKey,
		noneKey:   noneKey,
	}
}

func (f *mcpFixture) call(t *testing.T, credential string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return f.post(t, mcpPath, credential, body)
}

func (f *mcpFixture) post(t *testing.T, path, credential string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", mcpProtocolHeader)
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	rec := httptest.NewRecorder()
	f.srv.Web.ServeHTTP(rec, req)
	return rec
}

func (f *mcpFixture) get(t *testing.T, path, credential, requestID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	if requestID != "" {
		req.Header.Set(reqid.Header, requestID)
	}
	rec := httptest.NewRecorder()
	f.srv.Web.ServeHTTP(rec, req)
	return rec
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeRPC(t *testing.T, rec *httptest.ResponseRecorder) rpcResponse {
	t.Helper()
	var out rpcResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), "body=%s", rec.Body.String())
	return out
}

func scopeDeniedFor(t *testing.T, rec *httptest.ResponseRecorder, scope string) bool {
	t.Helper()
	resp := decodeRPC(t, rec)
	require.Nil(t, resp.Error, "a scope denial must answer in the result with isError, not the JSON-RPC envelope")

	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &result), "result=%s", string(resp.Result))
	if !result.IsError || len(result.Content) == 0 {
		return false
	}
	var payload rootmcp.ErrorPayload
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &payload), "content=%s", result.Content[0].Text)
	return payload.Code == apperror.CodeForbidden && payload.Meta["scope"] == scope
}

func listToolsBody() map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}
}

func callToolBody(name string, args map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	}
}

// TestMCP_RejectsAControlPlaneAudienceToken is the two-verifier guard.
func TestMCP_RejectsAControlPlaneAudienceToken(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})

	controlToken := f.issuer.mint(t, controlAudience, []string{authn.ScopeYasakuRead})
	mcpToken := f.issuer.mint(t, mcpAudience, []string{authn.ScopeYasakuRead})

	t.Run("the mcp-audience token is accepted at /mcp", func(t *testing.T) {
		rec := f.call(t, mcpToken, listToolsBody())
		require.Equal(t, http.StatusOK, rec.Code,
			"an mcp-audience token must authenticate at /mcp, or the rejection below proves nothing; body=%s", rec.Body.String())
	})

	t.Run("the control-plane token is rejected at /mcp", func(t *testing.T) {
		rec := f.call(t, controlToken, listToolsBody())
		require.Equal(t, http.StatusUnauthorized, rec.Code,
			"RFC 8707: a token minted for tokens.audience was accepted at /mcp — the mcp mount is not on its own verifier; body=%s", rec.Body.String())
		require.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=",
			"a 401 from /mcp must point the host at its RFC 9728 metadata document")
	})

	t.Run("the control-plane token is accepted at /api", func(t *testing.T) {
		require.False(t, credentialRejected(f.connect(t, controlToken)),
			"the control-plane token must authenticate at /api, or the split above is not an audience split")
	})

	t.Run("the mcp token is rejected at /api", func(t *testing.T) {
		require.True(t, credentialRejected(f.connect(t, mcpToken)),
			"RFC 8707: an mcp-audience token was accepted at the control plane")
	})
}

func credentialRejected(rec *httptest.ResponseRecorder) bool {
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return false
	}
	return rec.Code == http.StatusUnauthorized && body.Message == "unauthorized"
}

func (f *mcpFixture) connect(t *testing.T, credential string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, walletListProcedure, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer "+credential)
	rec := httptest.NewRecorder()
	f.srv.Web.ServeHTTP(rec, req)
	return rec
}

// TestMCP_ScopelessJWTCannotCallATool is the S7-vs-S2 guard in R4: the control plane lets a JWT principal skip its scope table because a signed-in human carries their own authority, but an MCP JWT is a delegated grant an agent holds, so every tool is scope-checked, JWT included.
func TestMCP_ScopelessJWTCannotCallATool(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})

	tests := []struct {
		name       string
		scopes     []string
		wantDenied bool
	}{
		{"no scopes at all", nil, true},
		{"an unrelated scope", []string{authn.ScopeAPIKeysRead}, true},
		{"the tool's own scope", []string{authn.ScopeYasakuRead}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := f.issuer.mint(t, mcpAudience, tt.scopes)
			rec := f.call(t, token, callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{}))
			require.Equal(t, http.StatusOK, rec.Code,
				"the JWT must authenticate, or this guard measures authentication rather than scope; body=%s", rec.Body.String())

			denied := scopeDeniedFor(t, rec, authn.ScopeYasakuRead)
			if tt.wantDenied {
				require.True(t, denied,
					"R4/S7: a JWT holding %v called list_wallets; an MCP JWT is scope-checked like a key; body=%s", tt.scopes, rec.Body.String())
				return
			}
			require.False(t, denied,
				"a JWT holding the tool's scope was denied it; body=%s", rec.Body.String())
		})
	}
}

// TestMCP_KeyScopeIsCheckedPerTool keeps the same rule true of the other credential class, and proves the surface reaches the shared Connect handlers rather than a second implementation.
func TestMCP_KeyScopeIsCheckedPerTool(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})

	t.Run("a key holding yasaku:read passes the scope check", func(t *testing.T) {
		rec := f.call(t, f.readKey, callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{}))
		require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
		require.False(t, scopeDeniedFor(t, rec, authn.ScopeYasakuRead), "body=%s", rec.Body.String())
	})

	t.Run("a key without yasaku:read is denied", func(t *testing.T) {
		rec := f.call(t, f.noneKey, callToolBody(yasakuv1mcp.ListWalletsToolName, map[string]any{}))
		require.True(t, scopeDeniedFor(t, rec, authn.ScopeYasakuRead),
			"a key lacking the tool's scope called it; body=%s", rec.Body.String())
	})
}

// TestMCP_CallWithoutArgumentsDecodes pins the nil-arguments normalization on the real protojson decode path.
func TestMCP_CallWithoutArgumentsDecodes(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	f.seedOwnWallet(t)

	token := f.issuer.mint(t, mcpAudience, []string{authn.ScopeYasakuRead})
	rec := f.call(t, token, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": yasakuv1mcp.ListWalletsToolName},
	})
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, toolResult(t, rec), "wallets",
		"the call did not reach the wallet service, so protojson was handed a nil body; body=%s", rec.Body.String())
}

func toolResult(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	resp := decodeRPC(t, rec)
	require.Nil(t, resp.Error, "the tool call was refused: %v", resp.Error)

	var result struct {
		IsError           bool           `json:"isError"`
		StructuredContent map[string]any `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &result), "result=%s", string(resp.Result))
	require.False(t, result.IsError, "the tool reported an error: %s", string(resp.Result))
	require.NotNil(t, result.StructuredContent, "the tool result carried no structured content: %s", string(resp.Result))
	return result.StructuredContent
}

func toolPayload(t *testing.T, rec *httptest.ResponseRecorder) rootmcp.ErrorPayload {
	t.Helper()
	resp := decodeRPC(t, rec)
	require.Nil(t, resp.Error, "a tool failure must answer in the result, not the JSON-RPC envelope")

	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &result), "result=%s", string(resp.Result))
	require.True(t, result.IsError, "the tool call succeeded; result=%s", string(resp.Result))
	require.NotEmpty(t, result.Content, "the tool error carried no content; result=%s", string(resp.Result))

	var payload rootmcp.ErrorPayload
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &payload), "content=%s", result.Content[0].Text)
	return payload
}

// TestMCP_UnauthenticatedCallIsChallenged keeps the surface fail-closed: no credential, an unparsable one and a credential of the wrong shape all answer one masked 401 carrying the RFC 9728 challenge.
func TestMCP_UnauthenticatedCallIsChallenged(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})

	for _, credential := range []string{"", "key_not_a_real_key", "not.a.token", "opaque-credential"} {
		t.Run("credential="+credential, func(t *testing.T) {
			rec := f.call(t, credential, listToolsBody())
			require.Equal(t, http.StatusUnauthorized, rec.Code,
				"/mcp admitted an unauthenticated call; body=%s", rec.Body.String())
			require.Contains(t, rec.Header().Get("WWW-Authenticate"), mcpMetadataPath,
				"the 401 must name the protected-resource metadata document")
			require.NotContains(t, rec.Body.String(), "tools",
				"an unauthenticated caller must not learn the tool catalog; body=%s", rec.Body.String())
		})
	}
}

// TestMCP_RunsTheMCPChainNotTheConsoleChain enforces R5 on boot's own assembly: /mcp carries the edge middlewares and none of the SSR ones.
func TestMCP_RunsTheMCPChainNotTheConsoleChain(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})

	require.NotEmpty(t, f.get(t, "/login", "", "").Header().Get("Content-Security-Policy"),
		"the console chain must set CSP, or this guard proves nothing")

	tests := []struct {
		name       string
		credential string
	}{
		{"unauthenticated", ""},
		{"authenticated", f.readKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const id = "mcp-chain-probe-0123456789"
			rec := f.call(t, tt.credential, listToolsBody())
			require.Empty(t, rec.Header().Get("Content-Security-Policy"),
				"/mcp ran the console chain: CSP is an SSR concern")
			require.Empty(t, rec.Header().Values("Set-Cookie"),
				"/mcp ran the console chain: a machine surface issues no session cookie")

			echoed := f.get(t, mcpPath, tt.credential, id)
			require.Equal(t, id, echoed.Header().Get(reqid.Header),
				"/mcp lost webmw.RequestID: it answered without echoing %s", reqid.Header)
			require.True(t, f.log.logged(mcpPath, id),
				"/mcp lost webmw.RequestLog: no http.request line carrying request id %q", id)

			minted := f.get(t, mcpPath, tt.credential, "").Header().Get(reqid.Header)
			require.NotEmpty(t, minted, "/mcp lost webmw.RequestID: it minted no id of its own")
			require.True(t, f.log.logged(mcpPath, minted),
				"/mcp lost webmw.RequestLog: no http.request line carrying minted id %q", minted)
		})
	}
}

// TestMCP_DisabledLeavesThePathUnmounted keeps mcp.enabled meaningful.
func TestMCP_DisabledLeavesThePathUnmounted(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: false})

	for _, path := range []string{mcpPath, mcpPath + "/", mcpMetadataPath} {
		t.Run(path, func(t *testing.T) {
			rec := f.post(t, path, f.readKey, listToolsBody())
			require.NotEqual(t, http.StatusOK, rec.Code,
				"%s answered while mcp.enabled is false; body=%s", path, rec.Body.String())
			require.NotContains(t, rec.Body.String(), yasakuv1mcp.ListWalletsToolName,
				"a disabled MCP surface published its tool catalog; body=%s", rec.Body.String())
		})
	}

	require.Nil(t, f.srv.MCP, "no MCP server may be built while mcp.enabled is false")
}

// TestMCP_ToolsCarryAScopeAndTheSharedInstances walks the registry boot actually built: no tool may reach a caller without a scope, and each must close over the same service instance /api mounts, so no verb has a second implementation.
func TestMCP_ToolsCarryAScopeAndTheSharedInstances(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	require.NotNil(t, f.srv.MCP, "mcp.enabled is true, so boot must have built a server")

	registry := f.srv.MCP.Registry()
	names := registry.Names()
	require.NotEmpty(t, names, "boot registered no MCP tools")

	instances := f.toolInstances()
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			spec, ok := registry.Spec(name)
			require.True(t, ok)
			require.NotEmpty(t, spec.Scope,
				"%s declares no scope; the root mcp package denies such a tool, and nothing here may default one", name)
			require.True(t, authn.Valid(spec.Scope),
				"%s declares %q, which is not in the scope catalog, so no key can ever hold it", name, spec.Scope)
			require.NotEmpty(t, spec.Description, "%s publishes no description for a host to read", name)

			want, known := instances[name]
			require.True(t, known, "%s is registered but this guard does not know which instance it must share", name)
			require.Same(t, want, registry.HandlerFor(name),
				"%s calls a service instance the Connect mount does not serve", name)
		})
	}
}

// TestMCP_ToolsShareTheConnectHandlerInstance is the identity guard the surface rests on: each tool must close over the very yasaku.v1 service the Connect mount serves. SECURITY: a second, independently constructed service — or a tool body calling straight into a domain package past Connect — would still satisfy the behavioral test below while duplicating a verb, and a duplicated verb is a verb whose tenant scoping and authorization can diverge.
func TestMCP_ToolsShareTheConnectHandlerInstance(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	require.NotNil(t, f.srv.MCP, "mcp.enabled is true, so boot must have built a server")
	require.NotNil(t, f.srv.API, "the control plane must be mounted, or there is no instance to share")

	registry := f.srv.MCP.Registry()
	instances := f.toolInstances()
	type toolInstance struct {
		tool string
		want any
	}
	tests := make([]toolInstance, 0, len(instances))
	for _, tool := range slices.Sorted(maps.Keys(instances)) {
		tests = append(tests, toolInstance{tool, instances[tool]})
	}

	covered := make([]string, 0, len(tests))
	for _, tt := range tests {
		covered = append(covered, tt.tool)
	}
	require.ElementsMatch(t, covered, registry.Names(),
		"boot registers a tool this guard names no shared instance for; every tool must be covered")

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			require.NotNil(t, tt.want,
				"the Connect mount holds no service for %s; an identity assertion between two nils proves nothing", tt.tool)
			got := registry.HandlerFor(tt.tool)
			require.NotNil(t, got, "%s is registered without the instance it calls", tt.tool)
			require.Same(t, tt.want, got,
				"%s calls a service instance the Connect mount does not serve, so the verb has two implementations", tt.tool)
			require.Equal(t, reflect.ValueOf(tt.want).Pointer(), reflect.ValueOf(got).Pointer(),
				"%s's handler and the Connect mount's handler are not the same address", tt.tool)
		})
	}
}

// TestMCP_ToolCallMatchesTheConnectCall is the behavioral complement: the same fixture driven through WalletService.CreateWallet over Connect and over the MCP tool must produce the same row. NOTE: this proves equivalence only.
func TestMCP_ToolCallMatchesTheConnectCall(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})

	scopes := []string{authn.ScopeYasakuRead, authn.ScopeYasakuWrite}
	controlToken := f.issuer.mint(t, controlAudience, scopes)
	mcpToken := f.issuer.mint(t, mcpAudience, scopes)

	created := f.connectRPC(t, controlToken, walletCreateProcedure,
		map[string]any{"name": "via-connect", "kind": "cash", "confirm": true})
	require.Equal(t, http.StatusOK, created.Code, "the Connect call must succeed; body=%s", created.Body.String())
	connectWallet := walletFromConnect(t, created)

	rec := f.call(t, mcpToken, callToolBody(yasakuv1mcp.CreateWalletToolName,
		map[string]any{"name": "via-mcp", "kind": "cash", "confirm": true}))
	require.Equal(t, http.StatusOK, rec.Code, "the tool call must succeed; body=%s", rec.Body.String())
	mcpWallet := walletFromTool(t, rec)

	t.Run("each call produced a real row", func(t *testing.T) {
		for name, w := range map[string]map[string]any{"connect": connectWallet, "mcp": mcpWallet} {
			require.NotEmpty(t, w["id"], "the %s call returned a wallet with no id", name)
			require.NotEmpty(t, w["createdAt"], "the %s call returned a wallet with no creation time", name)
		}
		require.Equal(t, "via-connect", connectWallet["name"])
		require.Equal(t, "via-mcp", mcpWallet["name"])
		require.ElementsMatch(t, slices.Collect(maps.Keys(connectWallet)), slices.Collect(maps.Keys(mcpWallet)),
			"the MCP tool emitted a different field set than the Connect call: connect=%v mcp=%v", connectWallet, mcpWallet)
	})

	t.Run("the two rows differ only in id, name and timestamps", func(t *testing.T) {
		require.Equal(t, stableWalletFields(connectWallet), stableWalletFields(mcpWallet),
			"the MCP tool and the Connect call disagree on the row they produced: connect=%v mcp=%v", connectWallet, mcpWallet)
	})

	t.Run("both rows are readable through the Connect mount", func(t *testing.T) {
		listed := f.connectRPC(t, controlToken, walletListProcedure, map[string]any{})
		require.Equal(t, http.StatusOK, listed.Code, "body=%s", listed.Body.String())

		var body struct {
			Wallets []map[string]any `json:"wallets"`
		}
		require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &body), "body=%s", listed.Body.String())

		names := make([]string, 0, len(body.Wallets))
		for _, w := range body.Wallets {
			name, _ := w["name"].(string)
			names = append(names, name)
		}
		require.ElementsMatch(t, []string{"via-connect", "via-mcp"}, names,
			"the tool wrote somewhere the Connect mount cannot read; names=%v", names)
	})
}

// TestMCP_ListToolsExposesEveryAnnotatedMethod pins the published tool names. NOTE: these names are a wire contract with every MCP host; renaming one breaks every host that saved it.
func TestMCP_ListToolsExposesEveryAnnotatedMethod(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})

	token := f.issuer.mint(t, mcpAudience, []string{authn.ScopeYasakuRead})
	rec := f.call(t, token, listToolsBody())
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	resp := decodeRPC(t, rec)
	require.Nil(t, resp.Error, "tools/list was refused: %v", resp.Error)
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &result), "result=%s", string(resp.Result))

	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	require.ElementsMatch(t, []string{
		"list_projects", "now",
		"list_wallets", "get_wallet", "wallet_totals", "create_wallet", "update_wallet", "archive_wallet", "adjust_balance",
		"list_categories", "create_category", "seed_default_categories",
		"list_recent_tx", "search_tx", "record_expense", "record_income", "record_transfer", "record_batch", "revise_tx", "delete_tx",
		"current_period", "list_periods", "preview_close", "close_period", "reopen_period",
		"period_report", "cashflow_report",
	}, names)
}

// TestMCP_RecordExpensePreviewThenConfirmWritesARow keeps the two-phase confirm: an unconfirmed mutation answers a preview and writes nothing.
func TestMCP_RecordExpensePreviewThenConfirmWritesARow(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})
	token := f.issuer.mint(t, mcpAudience, []string{authn.ScopeYasakuRead, authn.ScopeYasakuWrite})
	call := func(t *testing.T, name string, args map[string]any) map[string]any {
		t.Helper()
		rec := f.call(t, token, callToolBody(name, args))
		require.Equal(t, http.StatusOK, rec.Code, "%s: body=%s", name, rec.Body.String())
		return toolResult(t, rec)
	}

	wallet := call(t, yasakuv1mcp.CreateWalletToolName, map[string]any{
		"name":           "BCA",
		"kind":           "bank",
		"openingBalance": map[string]any{"amount": "1000000"},
		"confirm":        true,
	})
	require.NotNil(t, wallet["result"], "create_wallet must return the saved wallet: %v", wallet)

	listed := call(t, yasakuv1mcp.ListWalletsToolName, map[string]any{})
	wallets, ok := listed["wallets"].([]any)
	require.True(t, ok, "list_wallets payload: %v", listed)
	require.Len(t, wallets, 1)

	expense := map[string]any{
		"wallet": "BCA",
		"amount": map[string]any{"amount": "30000"},
		"note":   "kopi",
	}

	preview := call(t, yasakuv1mcp.RecordExpenseToolName, expense)
	require.NotNil(t, preview["preview"], "an unconfirmed call must return a preview: %v", preview)
	require.Nil(t, preview["result"], "a preview must not carry a saved result")

	before := call(t, yasakuv1mcp.ListRecentTxToolName, map[string]any{})
	require.Len(t, before["transactions"].([]any), 1, "only the opening balance may exist after a preview")

	expense["confirm"] = true
	confirmed := call(t, yasakuv1mcp.RecordExpenseToolName, expense)
	result, ok := confirmed["result"].(map[string]any)
	require.True(t, ok, "confirm must return a result: %v", confirmed)
	require.NotEmpty(t, result["id"], "the saved row must carry an id")

	after := call(t, yasakuv1mcp.ListRecentTxToolName, map[string]any{})
	rows, ok := after["transactions"].([]any)
	require.True(t, ok)
	require.Len(t, rows, 2, "the confirmed expense must be persisted alongside the opening balance")
}

func (f *mcpFixture) toolInstances() map[string]any {
	api := f.srv.API
	out := map[string]any{}
	for svc, names := range map[any][]string{
		api.WorkspaceSvc:   yasakuv1mcp.WorkspaceServiceToolNames(),
		api.WalletSvc:      yasakuv1mcp.WalletServiceToolNames(),
		api.CategorySvc:    yasakuv1mcp.CategoryServiceToolNames(),
		api.TransactionSvc: yasakuv1mcp.TransactionServiceToolNames(),
		api.PeriodSvc:      yasakuv1mcp.PeriodServiceToolNames(),
		api.ReportSvc:      yasakuv1mcp.ReportServiceToolNames(),
	} {
		for _, name := range names {
			out[name] = svc
		}
	}
	return out
}

func (f *mcpFixture) connectRPC(t *testing.T, credential, procedure string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer "+credential)
	rec := httptest.NewRecorder()
	f.srv.Web.ServeHTTP(rec, req)
	return rec
}

func walletFromConnect(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Result map[string]any `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	require.NotNil(t, body.Result, "the Connect response carried no saved wallet; body=%s", rec.Body.String())
	return body.Result
}

func walletFromTool(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	out := toolResult(t, rec)
	w, ok := out["result"].(map[string]any)
	require.True(t, ok, "the tool result carried no saved wallet: %v", out)
	return w
}

func stableWalletFields(w map[string]any) map[string]any {
	stable := maps.Clone(w)
	for _, field := range []string{"id", "name", "createdAt"} {
		delete(stable, field)
	}
	return stable
}

func needFieldsByTool() map[string][]string {
	target := []string{"org", "project"}
	with := func(fields ...string) []string { return append(slices.Clone(target), fields...) }
	return map[string][]string{
		yasakuv1mcp.CreateWalletToolName:          with("name", "kind"),
		yasakuv1mcp.UpdateWalletToolName:          with("wallet", "kind"),
		yasakuv1mcp.ArchiveWalletToolName:         with("wallet"),
		yasakuv1mcp.AdjustBalanceToolName:         with("wallet"),
		yasakuv1mcp.CreateCategoryToolName:        with("name", "icon", "color"),
		yasakuv1mcp.SeedDefaultCategoriesToolName: with(),
		yasakuv1mcp.RecordExpenseToolName:         with("wallet", "category", "period"),
		yasakuv1mcp.RecordIncomeToolName:          with("wallet", "category", "period"),
		yasakuv1mcp.RecordTransferToolName:        with("fromWallet", "toWallet", "period"),
		yasakuv1mcp.RecordBatchToolName:           with("items"),
		yasakuv1mcp.ReviseTxToolName:              with("wallet", "toWallet", "category", "period"),
		yasakuv1mcp.DeleteTxToolName:              with(),
		yasakuv1mcp.ClosePeriodToolName:           with("period"),
		yasakuv1mcp.ReopenPeriodToolName:          with("period"),
	}
}

// NOTE: mirrors NEED_FIELD in internal/mcp/ui/src/views/mutation_model.js, the only rename between needs[].field and a form's name=.
func needFormName(field string) string {
	switch field {
	case "org":
		return "target.org"
	case "project":
		return "target.project"
	}
	return field
}

func toolsCarryingNeeds(t *testing.T) []string {
	t.Helper()
	var out []string
	protoregistry.GlobalFiles.RangeFilesByPackage("yasaku.v1", func(fd protoreflect.FileDescriptor) bool {
		for i := range fd.Services().Len() {
			methods := fd.Services().Get(i).Methods()
			for j := range methods.Len() {
				m := methods.Get(j)
				tool, ok := proto.GetExtension(m.Options(), mcpv1.E_Tool).(*mcpv1.Tool)
				if !ok || tool.GetName() == "" || m.Output().Fields().ByName("needs") == nil {
					continue
				}
				out = append(out, tool.GetName())
			}
		}
		return true
	})
	slices.Sort(out)
	return out
}

var reNeedField = regexp.MustCompile(`needs\("(\w+)"|resolve\w*\([^()]*"(\w+)",|return "(\w+)"\n\t}\n\treturn "(\w+)"`)

func emittedNeedFields(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "controlplane", "*.go"))
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path) //nolint:gosec // fixed repo path
		require.NoError(t, err)
		for _, m := range reNeedField.FindAllStringSubmatch(string(src), -1) {
			for _, g := range m[1:] {
				if g != "" {
					seen[g] = true
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func schemaHasPath(schema map[string]any, path string) bool {
	node := schema
	for _, part := range strings.Split(path, ".") {
		props, _ := node["properties"].(map[string]any)
		next, ok := props[part].(map[string]any)
		if !ok {
			return false
		}
		node = next
	}
	return true
}

// TestEveryViewFormFieldExistsInItsToolSchema keeps the needs-phase form submittable: every name= it renders is a needs[].field the server emits, and a strict host refuses any argument its tool's input schema does not declare.
func TestEveryViewFormFieldExistsInItsToolSchema(t *testing.T) {
	table := needFieldsByTool()
	require.ElementsMatch(t, toolsCarryingNeeds(t), slices.Collect(maps.Keys(table)),
		"a tool whose response carries needs has no row in needFieldsByTool, or a row names a tool that cannot return needs")

	covered := map[string]bool{}
	for _, fields := range table {
		for _, f := range fields {
			covered[f] = true
		}
	}
	emitted := emittedNeedFields(t)
	require.NotEmpty(t, emitted, "no needs field found in internal/controlplane; the scan is broken")
	for _, f := range emitted {
		require.True(t, covered[f], "internal/controlplane emits needs field %q, which no row of needFieldsByTool names", f)
	}

	f := newMCPFixture(t, mcpOpts{enabled: true})
	rec := f.call(t, f.issuer.mint(t, mcpAudience, []string{authn.ScopeYasakuRead}), listToolsBody())
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	var listed struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(decodeRPC(t, rec).Result, &listed))
	schemas := map[string]map[string]any{}
	for _, tool := range listed.Tools {
		schemas[tool.Name] = tool.InputSchema
	}

	for _, tool := range slices.Sorted(maps.Keys(table)) {
		t.Run(tool, func(t *testing.T) {
			schema, ok := schemas[tool]
			require.True(t, ok, "tools/list does not publish %s", tool)
			for _, field := range table[tool] {
				name := needFormName(field)
				require.True(t, schemaHasPath(schema, name),
					"%s can answer needs[].field=%q, so its form submits %q, which its input schema does not declare", tool, field, name)
			}
		})
	}
}
