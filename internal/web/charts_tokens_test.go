package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestChartsReadNoRemovedTokens fails when charts.js reads a color variable or utility the daisyUI themes no longer define.
func TestChartsReadNoRemovedTokens(t *testing.T) {
	b, err := staticFS.ReadFile("static/charts.js")
	if err != nil {
		t.Fatalf("read charts.js: %v", err)
	}
	removed := regexp.MustCompile(`(?:--|text-|bg-|border-)(?:` + strings.Join(oldTokens(), "|") + `)(?:[^a-z0-9-]|$)`)
	if hits := removed.FindAllString(string(b), -1); len(hits) > 0 {
		t.Errorf("charts.js uses removed theme tokens %q; read daisyUI --color-* variables instead", hits)
	}
}
