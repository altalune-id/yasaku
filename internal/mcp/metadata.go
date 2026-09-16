package mcp

import (
	"encoding/json"
	"net/http"
	"strings"

	"altalune.id/yasaku/internal/platform/config"
	mcprt "altalune.id/yasaku/mcp"
)

const (
	wellKnownPath        = "/.well-known/oauth-protected-resource"
	metadataCacheControl = "public, max-age=300"
)

// protectedResource is the RFC 9728 protected-resource metadata document.
type protectedResource struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

func (s *Server) initMetadata(cfg *config.Config) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	basePath := strings.TrimRight(cfg.HTTP.BasePath, "/")
	// NOTE: RFC 9728 §3.1 inserts the resource's path before the well-known suffix, so an
	// unprefixed deployment still answers at /.well-known/oauth-protected-resource/mcp.
	pathScoped := wellKnownPath + basePath + "/mcp"
	s.routes = []string{wellKnownPath, pathScoped}
	s.challenge = `Bearer resource_metadata="` + strings.TrimRight(cfg.HTTP.BaseURL, "/") + pathScoped + `"`

	doc := protectedResource{
		Resource:               cfg.MCP.Audience,
		AuthorizationServers:   []string{cfg.Tokens.Issuer},
		ScopesSupported:        []string{string(mcprt.ScopeRead), string(mcprt.ScopeWrite)},
		BearerMethodsSupported: []string{"header"},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		panic("mcp: marshalling protected-resource metadata: " + err.Error())
	}
	s.metadata = body
}

// Metadata serves the RFC 9728 protected-resource document.
func (s *Server) Metadata() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", metadataCacheControl)
		_, _ = w.Write(s.metadata)
	})
}

// WellKnown returns the outer-mux patterns the metadata document answers on.
func (s *Server) WellKnown() map[string]http.Handler {
	h := s.Metadata()
	out := make(map[string]http.Handler, len(s.routes))
	for _, path := range s.routes {
		out["GET "+path] = h
	}
	return out
}
