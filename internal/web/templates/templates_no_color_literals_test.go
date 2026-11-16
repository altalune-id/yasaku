package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	darkVariant    = regexp.MustCompile(`(^|[\s"'\x60:])dark:`)
	arbitraryColor = regexp.MustCompile(`-\[(#|rgba?\(|hsla?\(|oklch\(|oklab\(|color-mix\(|color:)|(^|[\s"'\x60:!])\[color:|\b(bg|text|border(-[trblxyse])?|ring|outline|fill|stroke|from|via|to|decoration|accent|caret|divide|shadow|placeholder)-\[(var\(|[a-z]+\])`)
	physicalSide   = regexp.MustCompile(`(^|[\s"'\x60:!])-?((scroll-)?(ml|mr|pl|pr)-[\w.\[]|(left|right)-[\w.\[]|(text|float|clear)-(left|right)\b|border-[lr](-[\w\[]|\b)|rounded-([lr]|tl|tr|bl|br)(-[\w\[]|\b))`)
	inlineStyle    = regexp.MustCompile(`<[a-zA-Z][^<>]*\sstyle=[^<>]*>`)
	styleAttr      = regexp.MustCompile(`"style"\s*:`)
	radialProgress = regexp.MustCompile(`class="[^"]*\bradial-progress\b`)
	rawPalette     = regexp.MustCompile(`\b(bg|text|border|ring|outline|divide|fill|stroke|from|via|to|shadow|decoration|accent|caret|placeholder)-((slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}|black|white)\b`)
	largeControl   = regexp.MustCompile(`(^|[\s"'\x60:])(btn|input|select|menu)-lg([\s"'\x60]|$)`)
	tw3Scale       = regexp.MustCompile(`(^|[\s"'\x60:])(shadow|rounded-sm)([\s"'\x60]|$)`)
)

// TestTemplatesUseThemeAwareLogicalClasses keeps every template and Go class helper theme-safe and RTL-safe.
func TestTemplatesUseThemeAwareLogicalClasses(t *testing.T) {
	root := repoRoot(t)
	files := classSourceFiles(t, root)
	if len(files) < 40 {
		t.Fatalf("scanned only %d class sources; the walk is broken", len(files))
	}
	for _, f := range files {
		src := readSource(t, root, f)
		for line := range strings.SplitSeq(src, "\n") {
			if darkVariant.MatchString(line) {
				t.Errorf("%s: dark: variant (use daisyUI semantic colors): %s", f, oneLine(line))
			}
			if arbitraryColor.MatchString(line) {
				t.Errorf("%s: arbitrary color value: %s", f, oneLine(line))
			}
			if physicalSide.MatchString(line) {
				t.Errorf("%s: physical side utility (use ms-/me-/ps-/pe-/start-/end-, text-start/end, border-s/e, rounded-s/e): %s", f, oneLine(line))
			}
			if styleAttr.MatchString(line) {
				t.Errorf("%s: inline style in attributes: %s", f, oneLine(line))
			}
		}
		for _, tag := range inlineStyle.FindAllString(src, -1) {
			if !radialProgress.MatchString(tag) {
				t.Errorf("%s: inline style: %s", f, oneLine(tag))
			}
		}
	}
}

func classSourceFiles(t *testing.T, root string) []string {
	t.Helper()
	files := templFiles(t, root)
	for _, dir := range []string{"internal/web/templates", "internal/web"} {
		gos, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range gos {
			if strings.HasSuffix(g, "_test.go") || strings.HasSuffix(g, "_templ.go") {
				continue
			}
			rel, _ := filepath.Rel(root, g)
			files = append(files, rel)
		}
	}
	return files
}

func readSource(t *testing.T, root, f string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, f))
	if err != nil {
		t.Fatal(err)
	}
	return comment.ReplaceAllString(string(raw), "")
}
