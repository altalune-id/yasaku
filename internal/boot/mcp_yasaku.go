package boot

import (
	"maps"
	"net/http"

	"altalune.id/yasaku/internal/mcp/ui"
	rootmcp "altalune.id/yasaku/mcp"
)

const rootMetadataAliasRoute = "GET /.well-known/oauth-protected-resource"

// NOTE: app.css paints a transparent body and every card draws its own border, so a host-drawn frame would double up on every card.
func yasakuAppResource() rootmcp.UIResource {
	return rootmcp.UIResource{
		URI:           ui.ResourceURI,
		Name:          "yasaku",
		Body:          ui.Document(),
		PrefersBorder: new(bool),
	}
}

// NOTE: yasaku served the metadata document at the host-root well-known URI before the path-scoped one, and live MCP hosts still probe it.
func withRootMetadataAlias(s mcpSurface) mcpSurface {
	if s.Metadata == nil || "GET "+s.MetadataPath == rootMetadataAliasRoute {
		return s
	}
	routes := make(map[string]http.Handler, len(s.ChallengeRoutes)+1)
	maps.Copy(routes, s.ChallengeRoutes)
	routes[rootMetadataAliasRoute] = s.Metadata
	s.ChallengeRoutes = routes
	return s
}
