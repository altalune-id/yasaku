package web

import (
	"os"
	"strings"
	"testing"
)

func TestDirtyFormJS_ResyncsWithoutInputEvents(t *testing.T) {
	b, err := os.ReadFile("static/dirtyform.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{"'change'", "'focusin'", "'focusout'", "window.setTimeout(syncAll"} {
		if !strings.Contains(src, want) {
			t.Errorf("dirtyform.js must re-sync on %s so autofill without input events enables Save", want)
		}
	}
}
