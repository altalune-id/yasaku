package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

var (
	colorVar        = regexp.MustCompile(`--color-([a-z0-9-]+)\s*:`)
	classTokenSplit = regexp.MustCompile("[\\s\"'`{}(),]+")
)

const tokenUtilityPrefixes = `text|bg|border(?:-[trblxyse])?|ring(?:-offset)?|outline|divide|fill|stroke|from|via|to|accent|caret|decoration|shadow|placeholder`

func oldTokens() []string {
	return []string{
		"accent-foreground", "background", "border", "card", "card-foreground", "chart-1", "chart-2", "chart-3", "chart-4", "chart-5",
		"destructive", "destructive-foreground", "foreground", "input", "muted", "muted-foreground", "popover", "popover-foreground",
		"primary-foreground", "ring", "secondary-foreground", "sidebar", "sidebar-accent", "sidebar-accent-foreground", "sidebar-border",
		"sidebar-foreground", "sidebar-primary", "sidebar-primary-foreground", "sidebar-ring",
	}
}

// TestNoOldTokenUtilities fails when a template, Go file or script uses a utility named after an old theme token.
func TestNoOldTokenUtilities(t *testing.T) {
	if len(classSources(t)) == 0 {
		t.Fatal("found no templates, Go files or scripts to scan; the scan is broken")
	}
	var hits []string
	for class, files := range tokenUses(t, tokenUtilityPattern(oldTokens())) {
		hits = append(hits, class+" <- "+strings.Join(dedupe(files), ", "))
	}
	sort.Strings(hits)
	if len(hits) > 0 {
		t.Errorf("old token utilities; use daisyUI semantic colors:\n  %s", strings.Join(hits, "\n  "))
	}
}

// TestStylesheetDefinesNoOldToken fails when the Tailwind source defines a color variable named after an old theme token.
func TestStylesheetDefinesNoOldToken(t *testing.T) {
	src, err := os.ReadFile("ui/app.tailwind.css")
	if err != nil {
		t.Fatalf("read ui/app.tailwind.css: %v", err)
	}
	var defined []string
	for _, m := range colorVar.FindAllStringSubmatch(string(src), -1) {
		if slices.Contains(oldTokens(), m[1]) {
			defined = append(defined, m[1])
		}
	}
	if len(defined) > 0 {
		t.Errorf("ui/app.tailwind.css defines old theme tokens %v; delete them and use daisyUI --color-* variables", defined)
	}
}

func tokenUtilityPattern(tokens []string) *regexp.Regexp {
	quoted := make([]string, len(tokens))
	for i, tok := range tokens {
		quoted[i] = regexp.QuoteMeta(tok)
	}
	return regexp.MustCompile(`^(?:` + tokenUtilityPrefixes + `)-(?:` + strings.Join(quoted, "|") + `)(?:/\d{1,3})?$`)
}

func tokenUses(t *testing.T, utility *regexp.Regexp) map[string][]string {
	t.Helper()
	used := map[string][]string{}
	for _, src := range classSources(t) {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		for _, class := range classTokenSplit.Split(string(b), -1) {
			base := strings.TrimLeft(class[strings.LastIndex(class, ":")+1:], "!-")
			if utility.MatchString(base) {
				used[class] = append(used[class], filepath.Base(src))
			}
		}
	}
	return used
}

func classSources(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasSuffix(name, ".templ"):
			out = append(out, path)
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && !strings.HasSuffix(name, "_templ.go"):
			out = append(out, path)
		case filepath.Dir(path) == "static" && strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".min.js"):
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	return out
}

func selectorDefined(sheet, selector string) bool {
	for i := 0; ; {
		j := strings.Index(sheet[i:], selector)
		if j < 0 {
			return false
		}
		end := i + j + len(selector)
		if end >= len(sheet) || !isClassByte(sheet[end]) {
			return true
		}
		i = end
	}
}

func isClassByte(c byte) bool {
	return c == '-' || c == '_' || c == '\\' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func dedupe(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}
