package icons

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// NOTE: scraping the sources keeps this self-maintaining — icons.Icon renders an invisible
// <span data-missing-icon> for an unknown name, so an un-vendored icon otherwise ships as a gap.
var (
	reTemplIcon   = regexp.MustCompile(`icons\.Icon\(\s*"([a-z0-9-]+)"`)
	reAllowedList = regexp.MustCompile(`(?s)var AllowedIcons = \[\]string\{(.*?)\}`)
	reQuoted      = regexp.MustCompile(`"([a-z0-9-]+)"`)
)

func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func referencedIcons(t *testing.T) map[string]string {
	t.Helper()

	root := repoRoot(t)
	out := map[string]string{}

	templates, err := filepath.Glob(filepath.Join(root, "internal/web/templates/*.templ"))
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	for _, f := range templates {
		b, rErr := os.ReadFile(f)
		if rErr != nil {
			t.Fatalf("read %s: %v", f, rErr)
		}
		for _, m := range reTemplIcon.FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = filepath.Base(f)
		}
	}

	categoryFile := filepath.Join(root, "internal/category/category.go")
	b, err := os.ReadFile(categoryFile)
	if err != nil {
		t.Fatalf("read %s: %v", categoryFile, err)
	}
	block := reAllowedList.FindSubmatch(b)
	if block == nil {
		t.Fatal("category.AllowedIcons not found — the scraper pattern has gone stale")
	}
	for _, m := range reQuoted.FindAllStringSubmatch(string(block[1]), -1) {
		out[m[1]] = "category.AllowedIcons"
	}

	if len(out) == 0 {
		t.Fatal("found no icon references — the scraper patterns have gone stale")
	}
	return out
}

func TestEveryReferencedIconIsVendored(t *testing.T) {
	t.Parallel()

	refs := referencedIcons(t)
	names := make([]string, 0, len(refs))
	for n := range refs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			if !Has(n) {
				t.Fatalf("icon %q is referenced by %s but is not vendored — run: make icons-add NAME=%s", n, refs[n], n)
			}
			if inner, ok := InnerFor(n); !ok || inner == "" {
				t.Fatalf("icon %q has an empty SVG body", n)
			}
		})
	}
}

// SECURITY: every vendored body is injected with templ.Raw, so a script or handler attribute would execute.
func TestVendoredIconsCarryNoExecutableMarkup(t *testing.T) {
	t.Parallel()

	for _, n := range Names() {
		t.Run(n, func(t *testing.T) {
			inner, ok := InnerFor(n)
			if !ok {
				t.Fatalf("icon %q vanished between Names and InnerFor", n)
			}
			lower := strings.ToLower(inner)
			for _, bad := range []string{"<script", "javascript:", "onload=", "onerror=", "onclick=", "<foreignobject"} {
				if strings.Contains(lower, bad) {
					t.Fatalf("icon %q contains %q", n, bad)
				}
			}
		})
	}
}
