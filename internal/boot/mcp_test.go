package boot_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/logger"
)

const (
	mcpSubject   = "sub-mcp"
	mcpBearer    = "stub-token"
	mcpToolCount = 27
)

type mcpStubVerifier struct{ principal session.Principal }

func (v mcpStubVerifier) Verify(context.Context, string) (session.Principal, error) {
	return v.principal, nil
}

type bearerRoundTripper struct{ token string }

func (b bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(clone)
}

// newIssuerStub serves just enough OIDC discovery for tokens.NewVerifier to build; the JWKS is
// never fetched because WithMCPVerifier replaces the verifier that would use it.
func newIssuerStub(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	return srv.URL
}

type mcpHarness struct {
	boot    *boot.Server
	web     *httptest.Server
	session *sdk.ClientSession
}

func newMCPHarness(t *testing.T) *mcpHarness {
	t.Helper()
	ctx := t.Context()
	issuer := newIssuerStub(t)

	cfg := &config.Config{
		Mode: config.ModeSelfhosted,
		HTTP: config.HTTPConfig{Addr: "127.0.0.1:0", BaseURL: "http://127.0.0.1"},
		DB: db.DBConfig{
			Driver:      db.DriverSQLite,
			DSN:         filepath.Join(t.TempDir(), "mcp.db"),
			TablePrefix: "yasaku_",
			Schema:      "public",
			AutoMigrate: true,
		},
		Tenant: config.TenantConfig{
			SingletonOrg:            config.SingletonOrgConfig{Slug: "default", Name: "Default"},
			PersonalOrgSlugFallback: "personal",
			PersonalProjectSlug:     "default",
		},
		MCP:  config.MCPConfig{Enabled: true, Audience: "http://127.0.0.1/mcp"},
		Log:  logger.Config{Level: "error", Format: "json"},
		Mail: config.MailConfig{Driver: "console", From: "no-reply@example.com"},
	}
	cfg.Tokens.Issuer = issuer
	cfg.Tokens.Audience = "urn:yasaku:api"
	require.NoError(t, cfg.Validate(), "the harness config must satisfy the mcp invariants")

	principal := session.Principal{
		IDPIssuer:  issuer,
		IDPSubject: mcpSubject,
		Scopes:     []string{"yasaku:read", "yasaku:write"},
	}
	srv, err := boot.BootServer(ctx, cfg,
		boot.WithLogger(logger.New(cfg.Log)),
		boot.WithScheduler(false),
		boot.WithMCPVerifier(mcpStubVerifier{principal: principal}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })

	u, err := srv.Users.EnsureFromOIDC(ctx, user.Claims{
		Issuer:  issuer,
		Subject: mcpSubject,
		Email:   "owner@example.com",
		Name:    "Owner",
	})
	require.NoError(t, err)

	o, err := srv.Orgs.BootstrapSingleton(ctx, cfg.Tenant.SingletonOrg.Slug, cfg.Tenant.SingletonOrg.Name, u.ID)
	require.NoError(t, err)
	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: u.ID})
	_, err = srv.Projects.BootstrapSystem(orgCtx, o.ID, "main", "Main")
	require.NoError(t, err)

	web := httptest.NewServer(srv.Web)
	t.Cleanup(web.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "yasaku-test", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:             web.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerRoundTripper{token: mcpBearer}},
		DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err, "the MCP client must reach the mounted endpoint")
	t.Cleanup(func() { _ = cs.Close() })

	return &mcpHarness{boot: srv, web: web, session: cs}
}

func (h *mcpHarness) call(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	res, err := h.session.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err, "CallTool(%s)", name)
	require.Len(t, res.Content, 1)
	text, ok := res.Content[0].(*sdk.TextContent)
	require.True(t, ok, "want text content, got %T", res.Content[0])
	require.False(t, res.IsError, "CallTool(%s) failed: %s", name, text.Text)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out), "tool output: %s", text.Text)
	return out
}

func TestMCP_ListToolsExposesEveryAnnotatedMethod(t *testing.T) {
	h := newMCPHarness(t)

	res, err := h.session.ListTools(t.Context(), &sdk.ListToolsParams{})
	require.NoError(t, err)

	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)

	require.Len(t, names, mcpToolCount)
	for _, want := range []string{"list_wallets", "record_expense", "close_period", "period_report"} {
		require.Contains(t, names, want)
	}
}

func TestMCP_RecordExpensePreviewThenConfirmWritesARow(t *testing.T) {
	h := newMCPHarness(t)

	wallet := h.call(t, "create_wallet", map[string]any{
		"name":           "BCA",
		"kind":           "bank",
		"openingBalance": map[string]any{"amount": "1000000"},
		"confirm":        true,
	})
	require.NotNil(t, wallet["result"], "create_wallet must return the saved wallet: %v", wallet)

	listed := h.call(t, "list_wallets", map[string]any{})
	wallets, ok := listed["wallets"].([]any)
	require.True(t, ok, "list_wallets payload: %v", listed)
	require.Len(t, wallets, 1)

	expense := map[string]any{
		"wallet": "BCA",
		"amount": map[string]any{"amount": "30000"},
		"note":   "kopi",
	}

	preview := h.call(t, "record_expense", expense)
	require.NotNil(t, preview["preview"], "an unconfirmed call must return a preview: %v", preview)
	require.Nil(t, preview["result"], "a preview must not carry a saved result")

	before := h.call(t, "list_recent_tx", map[string]any{})
	require.Len(t, before["transactions"].([]any), 1, "only the opening balance may exist after a preview")

	expense["confirm"] = true
	confirmed := h.call(t, "record_expense", expense)
	result, ok := confirmed["result"].(map[string]any)
	require.True(t, ok, "confirm must return a result: %v", confirmed)
	require.NotEmpty(t, result["id"], "the saved row must carry an id")

	after := h.call(t, "list_recent_tx", map[string]any{})
	rows, ok := after["transactions"].([]any)
	require.True(t, ok)
	require.Len(t, rows, 2, "the confirmed expense must be persisted alongside the opening balance")
}

func TestMCP_MetadataAndChallengeAreServedByTheWebMux(t *testing.T) {
	h := newMCPHarness(t)

	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
	} {
		resp, err := http.Get(h.web.URL + path) //nolint:noctx // short-lived test request
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		require.Equal(t, "application/json", resp.Header.Get("Content-Type"), path)

		var doc map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))
		require.Equal(t, "http://127.0.0.1/mcp", doc["resource"], path)
		require.Equal(t, []any{"yasaku:read", "yasaku:write"}, doc["scopes_supported"], path)
	}

	resp, err := http.Post(h.web.URL+"/mcp", "application/json", http.NoBody) //nolint:noctx // short-lived test request
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, resp.Header.Get("WWW-Authenticate"), "resource_metadata=")
}

func TestMCP_DisabledByDefaultLeavesNoRoutes(t *testing.T) {
	cfg := newSmokeCfg(t)
	srv, err := boot.BootServer(t.Context(), cfg, boot.WithScheduler(false))
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })

	rec := httptest.NewRecorder()
	srv.Web.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	require.NotEqual(t, http.StatusUnauthorized, rec.Code, "mcp must not answer while disabled")

	rec = httptest.NewRecorder()
	srv.Web.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}
