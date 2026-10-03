package mcp

import (
	"testing"

	"altalune.id/yasaku/internal/platform/authn"
)

func TestEveryYasakuToolHasAnEnforcedAndAFineScope(t *testing.T) {
	enforced := yasakuToolScopes()
	fine := YasakuFineToolScopes()
	if len(enforced) != 27 {
		t.Fatalf("yasaku publishes 27 tools, the enforced table has %d", len(enforced))
	}
	for tool, scope := range enforced {
		if scope != authn.ScopeYasakuRead && scope != authn.ScopeYasakuWrite {
			t.Errorf("%s: enforced scope %q, want yasaku:read or yasaku:write", tool, scope)
		}
		if fine[tool] == "" {
			t.Errorf("%s: no fine scope declared", tool)
		}
	}
	for tool := range fine {
		if _, ok := enforced[tool]; !ok {
			t.Errorf("%s: fine scope for a tool that is not published", tool)
		}
	}
}

func TestFineScopesAreNotMintable(t *testing.T) {
	for _, s := range YasakuFineToolScopes() {
		if authn.Valid(s) {
			t.Errorf("%s is in the catalogue; fine scopes stay TODO until the cutover", s)
		}
	}
}
