package boot

import (
	"net/http"

	"altalune.id/yasaku/internal/mcp/ui"
	rootmcp "altalune.id/yasaku/mcp"
)

const rootMetadataAliasPath = "/.well-known/oauth-protected-resource"

// NOTE: app.css paints a transparent body and every card draws its own border, so a host-drawn frame would double up on every card.
func yasakuAppResource() rootmcp.UIResource {
	return rootmcp.UIResource{
		URI:           ui.ResourceURI,
		Name:          "yasaku",
		Body:          ui.Document(),
		PrefersBorder: new(bool),
	}
}

// NOTE: the host-root alias is non-canonical under RFC 9728 Section 3.3.
func withRootMetadataAlias(routes map[string]http.Handler, s mcpSurface) map[string]http.Handler {
	if s.Metadata == nil || s.MetadataPath == rootMetadataAliasPath {
		return routes
	}
	if _, taken := routes[rootMetadataAliasPath]; taken {
		return routes
	}
	routes[rootMetadataAliasPath] = s.Metadata
	return routes
}
