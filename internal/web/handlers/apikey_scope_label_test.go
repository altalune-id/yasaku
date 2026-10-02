package handlers_test

import (
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/web/handlers"
)

func TestEveryCatalogueScopeHasAnEnUSLabel(t *testing.T) {
	t.Parallel()
	tr := i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)
	for _, s := range authn.AllScopes() {
		key := handlers.ScopeLabelKey(s)
		if got := tr.T(key); got == "" || got == key {
			t.Errorf("scope %q has no en-US label for %q", s, key)
		}
	}
}
