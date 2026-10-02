package boot_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tokens"
)

const rootMetadataPath = "/.well-known/oauth-protected-resource"

// TestMCP_RootMetadataAliasServesTheSameDocument keeps the host-root well-known URI yasaku served before the sync: live MCP hosts probe it.
func TestMCP_RootMetadataAliasServesTheSameDocument(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: true})

	scoped := f.get(t, mcpMetadataPath, "", "")
	require.Equal(t, http.StatusOK, scoped.Code, "body=%s", scoped.Body.String())
	root := f.get(t, rootMetadataPath, "", "")
	require.Equal(t, http.StatusOK, root.Code, "body=%s", root.Body.String())

	require.JSONEq(t, scoped.Body.String(), root.Body.String(),
		"the root alias must serve the path-scoped metadata document unchanged")
	require.Equal(t, scoped.Header().Get("Content-Type"), root.Header().Get("Content-Type"))
	require.Empty(t, root.Header().Get("Content-Security-Policy"), "the alias ran the console chain")
}

func TestMCP_RootMetadataAliasIsUnmountedWithMCP(t *testing.T) {
	f := newMCPFixture(t, mcpOpts{enabled: false})
	require.NotEqual(t, http.StatusOK, f.get(t, rootMetadataPath, "", "").Code,
		"the root alias answered while mcp.enabled is false")
}

func TestMCP_SurfaceIsNotBehindTheOnboardingGate(t *testing.T) {
	for _, basePath := range []string{"", "/app"} {
		t.Run("base="+basePath, func(t *testing.T) {
			issuer := newTokenIssuer(t)
			cfg := newSmokeCfg(t)
			cfg.Mode = config.ModeCloud
			cfg.HTTP.BasePath = basePath
			cfg.OIDC = config.OIDCConfig{Issuer: stubIssuer(t), ClientID: "mcp-client", ClientSecret: "mcp-secret"}
			cfg.API.Enabled = true
			cfg.API.KeyPrefix = apikey.DefaultPrefix
			cfg.Tokens = tokens.Config{Issuer: issuer.url, Audience: "http://127.0.0.1" + basePath + "/api"}
			cfg.MCP = config.MCPConfig{Enabled: true, Audience: "http://127.0.0.1" + basePath + mcpPath}

			srv, err := boot.BootServer(context.Background(), cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = srv.Close() })
			require.False(t, srv.Onboarded, "the server must still be before onboarding")

			serve := func(method, path string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", mcpProtocolHeader)
				rec := httptest.NewRecorder()
				srv.Web.ServeHTTP(rec, req)
				return rec
			}

			console := serve(http.MethodGet, basePath+"/")
			require.Equal(t, http.StatusSeeOther, console.Code, "the console must be gated before onboarding")

			mcp := serve(http.MethodPost, basePath+mcpPath)
			require.Equal(t, http.StatusUnauthorized, mcp.Code, "body=%s", mcp.Body.String())
			require.Contains(t, mcp.Header().Get("WWW-Authenticate"), "resource_metadata=")
			require.Empty(t, mcp.Header().Get("Location"))

			for _, path := range []string{rootMetadataPath, "/.well-known/oauth-protected-resource" + basePath + mcpPath} {
				rec := serve(http.MethodGet, path)
				require.Equal(t, http.StatusOK, rec.Code, "path=%s body=%s", path, rec.Body.String())
			}
		})
	}
}
