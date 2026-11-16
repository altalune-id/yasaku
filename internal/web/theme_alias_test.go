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
	aliasBlock      = regexp.MustCompile(`(?s)@theme inline \{(.*?)\n\}`)
	aliasName       = regexp.MustCompile(`--color-([a-z0-9-]+):`)
	classTokenSplit = regexp.MustCompile("[\\s\"'`{}(),]+")
)

const tokenUtilityPrefixes = `text|bg|border(?:-[trblxyse])?|ring(?:-offset)?|outline|divide|fill|stroke|from|via|to|accent|caret|decoration|shadow|placeholder`

func aliasAllowlist() []string {
	return []string{
		"background", "border", "card", "card-foreground", "destructive", "destructive-foreground", "foreground",
		"input", "muted", "muted-foreground", "popover", "popover-foreground", "primary-foreground", "ring",
	}
}

func oldTokensWithoutAlias() []string {
	return []string{
		"accent-foreground", "chart-1", "chart-2", "chart-3", "chart-4", "chart-5", "secondary-foreground",
		"sidebar", "sidebar-accent", "sidebar-accent-foreground", "sidebar-border", "sidebar-foreground",
		"sidebar-primary", "sidebar-primary-foreground", "sidebar-ring",
	}
}

// TestAliasBlockMatchesAllowlist fails when the temporary alias block gains or loses a name the allowlist does not.
func TestAliasBlockMatchesAllowlist(t *testing.T) {
	got := aliasNames(t)
	want := aliasAllowlist()
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("@theme inline aliases = %v, allowlist = %v; the alias layer only shrinks", got, want)
	}
}

// TestNoOldTokenOutsideAllowlist fails when a template uses an old token name that the alias layer does not map.
func TestNoOldTokenOutsideAllowlist(t *testing.T) {
	banned := tokenUtilityPattern(oldTokensWithoutAlias())
	var hits []string
	for class, files := range tokenUses(t, banned) {
		hits = append(hits, class+" <- "+strings.Join(dedupe(files), ", "))
	}
	sort.Strings(hits)
	if len(hits) > 0 {
		t.Errorf("old token utilities outside the alias allowlist; use daisyUI semantic colors:\n  %s", strings.Join(hits, "\n  "))
	}
}

// TestAliasUtilitiesAreDefined fails when a template uses an alias utility that the compiled stylesheet lacks.
func TestAliasUtilitiesAreDefined(t *testing.T) {
	used := tokenUses(t, tokenUtilityPattern(aliasNames(t)))
	if len(used) == 0 {
		t.Fatal("found no alias utilities in the templates; the scan is broken")
	}
	var css strings.Builder
	for _, name := range []string{"static/app.css", "static/themes.css"} {
		b, err := staticFS.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		css.Write(b)
	}
	sheet := css.String()
	var missing []string
	for class, files := range used {
		if !selectorDefined(sheet, "."+escapeClass(class)) {
			missing = append(missing, class+" <- "+strings.Join(dedupe(files), ", "))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d alias utilities are used but defined in no stylesheet; check the @theme block in internal/web/ui/app.tailwind.css and run `make ui-vendor`:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func aliasNames(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("ui/app.tailwind.css")
	if err != nil {
		t.Fatalf("read ui/app.tailwind.css: %v", err)
	}
	block := aliasBlock.FindStringSubmatch(string(src))
	if block == nil {
		t.Fatal("no @theme inline block in ui/app.tailwind.css")
	}
	var names []string
	for _, m := range aliasName.FindAllStringSubmatch(block[1], -1) {
		names = append(names, m[1])
	}
	sort.Strings(names)
	return names
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

func escapeClass(class string) string {
	var b strings.Builder
	for i := 0; i < len(class); i++ {
		if !isClassByte(class[i]) || class[i] == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(class[i])
	}
	return b.String()
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
