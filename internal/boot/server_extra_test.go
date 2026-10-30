package boot

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform"
	"altalune.id/yasaku/internal/platform/config"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestBootClient_Wires(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	c, err := BootClient(context.Background(), cfg, "http://127.0.0.1:0", "sometoken")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.NotNil(t, c.Log)
	assert.NotNil(t, c.Conn)
	assert.NotNil(t, c.Reporter)
	assert.Equal(t, cfg, c.Cfg)
}

func TestBuildOIDCClient_NilWhenOIDCUnset(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.OIDC.Issuer = ""
	rp, secret, err := buildOIDCClient(cfg, nil, discardLogger())
	require.NoError(t, err)
	assert.Nil(t, rp)
	assert.Nil(t, secret)
}

type warnRecorder struct {
	mu    sync.Mutex
	warns []string
}

func (w *warnRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (w *warnRecorder) Handle(_ context.Context, r slog.Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if r.Level == slog.LevelWarn {
		w.warns = append(w.warns, r.Message)
	}
	return nil
}

func (w *warnRecorder) WithAttrs([]slog.Attr) slog.Handler { return w }

func (w *warnRecorder) WithGroup(string) slog.Handler { return w }

func TestResolveStateSecret(t *testing.T) {
	t.Parallel()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	tests := []struct {
		name    string
		secret  string
		want    []byte
		wantErr bool
	}{
		{"base64url", base64.RawURLEncoding.EncodeToString(raw), raw, false},
		{"base64 std", base64.StdEncoding.EncodeToString(raw), raw, false},
		{"too short", base64.RawURLEncoding.EncodeToString([]byte("short")), nil, true},
		{"not base64", "not-base64!!$", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.HTTP.StateSecret = tt.secret
			got, err := resolveStateSecret(cfg, discardLogger())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveStateSecret_EmptyMintsAnEphemeralKeyAndWarns(t *testing.T) {
	t.Parallel()
	rec := &warnRecorder{}
	got, err := resolveStateSecret(&config.Config{}, slog.New(rec))
	require.NoError(t, err)
	assert.Len(t, got, 32)
	assert.NotEqual(t, make([]byte, 32), got, "the ephemeral key must be random, never zero")
	require.Len(t, rec.warns, 1)
	assert.Contains(t, rec.warns[0], "using an ephemeral secret")
}

func TestBuildOIDCClient_RefusesAShortStateSecret(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.OIDC.Issuer = "https://issuer.example.com"
	cfg.HTTP.StateSecret = base64.RawURLEncoding.EncodeToString([]byte("short"))
	_, _, err := buildOIDCClient(cfg, nil, discardLogger())
	require.Error(t, err)
}

func TestOIDCHTTPClient_RefusesRedirects(t *testing.T) {
	t.Parallel()
	c := oidcHTTPClient()
	require.NotNil(t, c.CheckRedirect)
	assert.ErrorIs(t, c.CheckRedirect(nil, nil), http.ErrUseLastResponse)
}

// TestOIDCRedirectURL pins the /oauth/callback path so a typo can't silently break OIDC login.
func TestOIDCRedirectURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		baseURL string
		base    string
		want    string
	}{
		{"empty base URL returns empty", "", "", ""},
		{"root mount", "http://127.0.0.1:5150", "", "http://127.0.0.1:5150/oauth/callback"},
		{"trailing slash trimmed", "http://127.0.0.1:5150/", "", "http://127.0.0.1:5150/oauth/callback"},
		{"basePath preserved", "https://app.example.com", "/yasaku", "https://app.example.com/yasaku/oauth/callback"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{}
			cfg.HTTP.BaseURL = tc.baseURL
			cfg.HTTP.BasePath = tc.base
			assert.Equal(t, tc.want, oidcRedirectURL(cfg))
		})
	}
}

func TestOnboardPolicyFrom_ModeSelfhosted(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Mode = config.ModeSelfhosted
	pol := onboardPolicyFrom(cfg)
	assert.NotEmpty(t, pol.Mode)
}

func TestOnboardPolicyFrom_ModeCloud(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Mode = config.ModeCloud
	pol := onboardPolicyFrom(cfg)
	assert.NotEmpty(t, pol.Mode)
}

func TestAPIResource_ResolvedOnceAndWarnsOnABadAudience(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.API.Enabled = true
	cfg.Tokens.Issuer = "https://issuer.example.com"
	cfg.Tokens.Audience = "https://app.example.com/api"

	res := apiResource(cfg, discardLogger())
	require.NotNil(t, res)
	srv, _ := buildAPIHandler(cfg, &platform.Kernel{Log: discardLogger()}, &Services{}, res)
	assert.Same(t, res, srv.Resource)

	cfg.Tokens.Audience = "not a url"
	rec := &warnRecorder{}
	assert.Nil(t, apiResource(cfg, slog.New(rec)))
	require.Len(t, rec.warns, 1)

	cfg.Tokens.Audience = "https://app.example.com/api"
	cfg.Tokens.Issuer = ""
	assert.Nil(t, apiResource(cfg, discardLogger()))
}
