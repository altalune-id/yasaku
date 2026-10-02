package mcp

import (
	"altalune.id/yasaku/internal/platform/authn"
)

// ScopeTable declares the scope a caller must hold for every tool this surface publishes. SECURITY: registration reads this table through ScopeFor, so the runtime check cannot drift from it; a tool missing here resolves to the empty scope, which the root mcp server denies.
func ScopeTable() authn.ScopeTable {
	return yasakuToolScopes()
}

// ScopeFor returns the scope tool name requires, or the empty scope for a tool absent from the catalog.
func ScopeFor(name string) string { return ScopeTable()[name] }
