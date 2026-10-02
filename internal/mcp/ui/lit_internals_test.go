package ui

import (
	"strings"
	"testing"
)

// TestLitResultBrandIsMintedOnlyByTheChartTag: a hand-built template result bypasses the html tag, so only the audited svg tag in src/charts.js may mint one.
func TestLitResultBrandIsMintedOnlyByTheChartTag(t *testing.T) {
	found := false
	for _, p := range scriptParts {
		if !strings.Contains(mustRead(t, p), "_$litType$") {
			continue
		}
		if p != "src/charts.js" {
			t.Errorf("%s mints a Lit template result by hand; only src/charts.js may", p)
			continue
		}
		found = true
	}
	if !found {
		t.Error("src/charts.js no longer mints the svg result; drop this guard or restore the tag")
	}
	if n := strings.Count(mustRead(t, "src/charts.js"), "_$litType$"); n != 1 {
		t.Errorf("src/charts.js mints the brand %d times, want 1", n)
	}
}
