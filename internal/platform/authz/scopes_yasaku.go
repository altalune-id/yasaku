package authz

import (
	"slices"

	"altalune.id/yasaku/internal/platform/authn"
)

func admits(scopes []string, scope string) bool {
	if slices.Contains(scopes, scope) {
		return true
	}
	return scope == authn.ScopeYasakuRead && slices.Contains(scopes, authn.ScopeYasakuWrite)
}

func granted(scopes []string) string {
	switch {
	case slices.Contains(scopes, authn.ScopeYasakuWrite):
		return "authz.granted_write"
	case slices.Contains(scopes, authn.ScopeYasakuRead):
		return "authz.granted_read"
	default:
		return "authz.granted_none"
	}
}
