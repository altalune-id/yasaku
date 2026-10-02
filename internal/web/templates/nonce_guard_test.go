package templates

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	htmxAttr   = regexp.MustCompile(`\shx-(get|post|put|patch|delete|trigger|target|swap|include|vals|boost|status)\b`)
	hxNonce    = regexp.MustCompile(`\shx-nonce=`)
	scriptOpen = regexp.MustCompile(`<script\b[^>]*>`)
	nonceAttr  = regexp.MustCompile(`\snonce=`)
	jsonScript = regexp.MustCompile(`type="application/(ld\+)?json"`)
	inlineOn   = regexp.MustCompile(`\son[a-z]+=`)
	hxOn       = regexp.MustCompile(`\shx-on[:-]`)
	tagOpen    = regexp.MustCompile(`<[a-zA-Z][^<>]*>`)
	comment    = regexp.MustCompile(`(?m)^\s*//.*$`)
)

// SECURITY: under an enforcing CSP these fail only at runtime in the browser, so the rule is checked here.
func TestTemplatesFollowTheNonceRules(t *testing.T) {
	root := repoRoot(t)
	files := templFiles(t, root)
	if len(files) == 0 {
		t.Fatalf("no templ files under %s", root)
	}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		src := comment.ReplaceAllString(string(raw), "")
		for _, tag := range tagOpen.FindAllString(src, -1) {
			if htmxAttr.MatchString(tag) && !hxNonce.MatchString(tag) {
				t.Errorf("%s: htmx element without hx-nonce: %s", f, oneLine(tag))
			}
			if inlineOn.MatchString(tag) {
				t.Errorf("%s: inline event handler: %s", f, oneLine(tag))
			}
			if hxOn.MatchString(tag) {
				t.Errorf("%s: hx-on is blocked without unsafe-eval: %s", f, oneLine(tag))
			}
		}
		for _, tag := range scriptOpen.FindAllString(src, -1) {
			if !nonceAttr.MatchString(tag) && !jsonScript.MatchString(tag) {
				t.Errorf("%s: <script> without nonce: %s", f, oneLine(tag))
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// NOTE: tmp/ is skipped as well as node_modules and dot directories: it holds local secrets and no templates.
func templFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if rel != "." && (name == "node_modules" || strings.HasPrefix(name, ".") || rel == "tmp") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".templ") {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
