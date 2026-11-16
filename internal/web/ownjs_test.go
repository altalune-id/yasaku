package web

import (
	"os"
	"strings"
	"testing"
)

func TestOwnScriptsFeedTheCSSBuildByOneGlob(t *testing.T) {
	for file, wants := range map[string][]string{
		"../../Makefile":      {"$(filter-out %.min.js,$(wildcard internal/web/static/*.js))"},
		"ui/app.tailwind.css": {`@source "../static/*.js";`, `@source not "../static/*.min.js";`},
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(b), want) {
				t.Errorf("%s must list own scripts as %s, so a new script is never missed", file, want)
			}
		}
		if strings.Contains(string(b), "dialog.js") || strings.Contains(string(b), "dialog,") {
			t.Errorf("%s names a script by hand", file)
		}
	}
}
