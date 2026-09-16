package config

import (
	"strings"
	"testing"
)

func TestConfig_MCPEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		basePath string
		want     string
	}{
		{name: "no base path", baseURL: "https://y.example", want: "https://y.example/mcp"},
		{name: "trailing slash trimmed", baseURL: "https://y.example/", want: "https://y.example/mcp"},
		{name: "base path", baseURL: "https://y.example", basePath: "/app", want: "https://y.example/app/mcp"},
		{name: "port", baseURL: "http://localhost:5150", want: "http://localhost:5150/mcp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{HTTP: HTTPConfig{BaseURL: tc.baseURL, BasePath: tc.basePath}}
			if got := c.MCPEndpoint(); got != tc.want {
				t.Fatalf("MCPEndpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidate_MCPInvariants(t *testing.T) {
	base := func() *Config {
		c := &Config{
			Mode:     ModeSelfhosted,
			HTTP:     HTTPConfig{BaseURL: "https://y.example"},
			DB:       validDB(),
			Genesis:  GenesisConfig{Email: "root@example.com", Password: "x"},
			Security: validSecurity(),
		}
		c.Tenant.SingletonOrg.Slug = "default"
		c.Tenant.SingletonOrg.Name = "Default Organization"
		return c
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
		check   func(*testing.T, *Config)
	}{
		{
			name:   "disabled needs nothing",
			mutate: func(*Config) {},
		},
		{
			name:    "enabled without a tokens issuer fails naming the env var",
			mutate:  func(c *Config) { c.MCP.Enabled = true },
			wantSub: "YASAKU_TOKENS_ISSUER",
		},
		{
			name: "enabled defaults the audience to the mcp endpoint",
			mutate: func(c *Config) {
				c.MCP.Enabled = true
				c.Tokens.Issuer = "https://idp.example"
			},
			check: func(t *testing.T, c *Config) {
				t.Helper()
				if c.MCP.Audience != "https://y.example/mcp" {
					t.Fatalf("mcp.audience = %q, want the MCPEndpoint default", c.MCP.Audience)
				}
				if c.MCP.Audience != c.MCPEndpoint() {
					t.Fatalf("mcp.audience %q != MCPEndpoint() %q", c.MCP.Audience, c.MCPEndpoint())
				}
			},
		},
		{
			name: "enabled defaults the audience under a base path",
			mutate: func(c *Config) {
				c.MCP.Enabled = true
				c.HTTP.BasePath = "/app"
				c.Tokens.Issuer = "https://idp.example"
			},
			check: func(t *testing.T, c *Config) {
				t.Helper()
				if c.MCP.Audience != "https://y.example/app/mcp" {
					t.Fatalf("mcp.audience = %q, want the base-path endpoint", c.MCP.Audience)
				}
			},
		},
		{
			name: "audience that disagrees with the endpoint fails",
			mutate: func(c *Config) {
				c.MCP.Enabled = true
				c.Tokens.Issuer = "https://idp.example"
				c.MCP.Audience = "https://other.example/mcp"
			},
			wantSub: "YASAKU_MCP_AUDIENCE_OVERRIDE",
		},
		{
			name: "audience that disagrees is allowed with the override",
			mutate: func(c *Config) {
				c.MCP.Enabled = true
				c.Tokens.Issuer = "https://idp.example"
				c.MCP.Audience = "https://other.example/mcp"
				c.MCP.AudienceOverride = true
			},
			check: func(t *testing.T, c *Config) {
				t.Helper()
				if c.MCP.Audience != "https://other.example/mcp" {
					t.Fatalf("override must not rewrite the audience, got %q", c.MCP.Audience)
				}
			},
		},
		{
			name: "relative audience fails even with the override",
			mutate: func(c *Config) {
				c.MCP.Enabled = true
				c.Tokens.Issuer = "https://idp.example"
				c.MCP.Audience = "/mcp"
				c.MCP.AudienceOverride = true
			},
			wantSub: "absolute http",
		},
		{
			name: "audience with a fragment fails",
			mutate: func(c *Config) {
				c.MCP.Enabled = true
				c.Tokens.Issuer = "https://idp.example"
				c.MCP.Audience = "https://other.example/mcp#frag"
				c.MCP.AudienceOverride = true
			},
			wantSub: "fragment",
		},
		{
			name: "non-http audience fails",
			mutate: func(c *Config) {
				c.MCP.Enabled = true
				c.Tokens.Issuer = "https://idp.example"
				c.MCP.Audience = "urn:yasaku:mcp"
				c.MCP.AudienceOverride = true
			},
			wantSub: "absolute http",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(c)
			err := c.Validate()
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				if tc.check != nil {
					tc.check(t, c)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("want error containing %q, got %v", tc.wantSub, err)
			}
		})
	}
}

func TestLoad_MCPDisabledByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	cfg, err := Load("", withCwdOverride(t, dir), withGenesisFallback(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MCP.Enabled {
		t.Fatal("mcp.enabled must default to false")
	}
}
