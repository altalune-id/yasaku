package templates

import (
	"context"
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"gopkg.in/yaml.v3"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func renderLayout(t *testing.T, pref web.ThemePref) string {
	t.Helper()
	d := web.LayoutData{Title: "T", Theme: pref, ThemeAttr: pref.Attr(), FamilyAttr: pref.FamilyAttr(), CurrentPath: "/here", Nonce: "n0nce"}
	d = web.WithContent(d, templ.NopComponent)
	var b strings.Builder
	if err := Layout(d).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var (
	htmlTag   = regexp.MustCompile(`<html[^>]*>`)
	themeMeta = regexp.MustCompile(`<meta name="theme-color"[^>]*>`)
)

func TestLayout_RendersServerSideTheme(t *testing.T) {
	f := web.DefaultFamily()
	for _, tc := range []struct {
		mode      string
		attr      string
		metas     int
		withMedia bool
	}{
		{web.ModeLight, ` data-theme="` + f + `-light"`, 1, false},
		{web.ModeDark, ` data-theme="` + f + `-dark"`, 1, false},
		{web.ModeSystem, "", 2, true},
	} {
		out := renderLayout(t, web.ThemePref{Family: f, Mode: tc.mode})
		tag := htmlTag.FindString(out)
		if tc.attr == "" && strings.Contains(tag, "data-theme") {
			t.Errorf("%s: <html> carries data-theme: %s", tc.mode, tag)
		}
		if tc.attr != "" && !strings.Contains(tag, tc.attr) {
			t.Errorf("%s: <html> = %s, want %s", tc.mode, tag, tc.attr)
		}
		metas := themeMeta.FindAllString(out, -1)
		if len(metas) != tc.metas {
			t.Errorf("%s: %d theme-color metas, want %d: %v", tc.mode, len(metas), tc.metas, metas)
		}
		for _, m := range metas {
			if strings.Contains(m, "media=") != tc.withMedia {
				t.Errorf("%s: meta %s media mismatch", tc.mode, m)
			}
		}
		for _, banned := range []string{"__yasakuApplyMode", "yasaku.mode", "localStorage", "theme-controller", "data-color-mode"} {
			if strings.Contains(out, banned) {
				t.Errorf("%s: layout still ships client-side mode code %q", tc.mode, banned)
			}
		}
	}
}

func TestLayout_SystemWithNonDefaultFamilyRendersDataFamily(t *testing.T) {
	tag := htmlTag.FindString(renderLayout(t, web.ThemePref{Family: "forest", Mode: web.ModeSystem}))
	if !strings.Contains(tag, ` data-family="forest"`) || strings.Contains(tag, "data-theme") {
		t.Errorf("System + forest: <html> = %s, want data-family and no data-theme", tag)
	}
	tag = htmlTag.FindString(renderLayout(t, web.ThemePref{Family: "forest", Mode: web.ModeDark}))
	if !strings.Contains(tag, ` data-theme="forest-dark"`) || strings.Contains(tag, "data-family") {
		t.Errorf("Dark + forest: <html> = %s, want data-theme and no data-family", tag)
	}
	if tag := htmlTag.FindString(renderLayout(t, web.ThemePref{Family: web.DefaultFamily(), Mode: web.ModeSystem})); strings.Contains(tag, "data-family") {
		t.Errorf("System + default family: <html> = %s, want no data-family", tag)
	}
}

func TestThemePicker_PlainFormWithCheckedMode(t *testing.T) {
	f := web.DefaultFamily()
	d := web.LayoutData{Theme: web.ThemePref{Family: "glacier", Mode: web.ModeDark}, CurrentPath: "/back"}
	var b strings.Builder
	if err := ThemePicker(d).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		`<form method="post" action="/theme"`,
		`name="redirect" value="/back"`,
		`type="submit"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("picker lacks %s:\n%s", want, out)
		}
	}
	radios := regexp.MustCompile(`<input type="radio" name="mode" value="([a-z]+)"[^>]*>`).FindAllStringSubmatch(out, -1)
	if len(radios) != 3 {
		t.Fatalf("want 3 mode radios, got %d", len(radios))
	}
	for _, r := range radios {
		checked := strings.Contains(r[0], " checked")
		if checked != (r[1] == web.ModeDark) {
			t.Errorf("radio %s checked=%v", r[1], checked)
		}
	}
	fams := regexp.MustCompile(`<input type="radio" name="family" value="([a-z]+)"[^>]*>`).FindAllStringSubmatch(out, -1)
	if len(fams) != len(web.Families()) {
		t.Fatalf("want %d family radios, got %d", len(web.Families()), len(fams))
	}
	for _, r := range fams {
		if strings.Contains(r[0], " checked") != (r[1] == "glacier") {
			t.Errorf("family radio %s checked mismatch", r[1])
		}
	}
	if !strings.Contains(out, `value="`+f+`"`) {
		t.Errorf("default family %s not offered", f)
	}
}

func TestThemeFamilies_TranslatedInEveryLocale(t *testing.T) {
	fsys := i18n.EmbeddedLocalesFS()
	files, err := fs.Glob(fsys, "locales/"+i18n.LocaleFilenamePrefix+"*"+i18n.LocaleFilenameSuffix)
	if err != nil || len(files) != 5 {
		t.Fatalf("locale files %v, %v", files, err)
	}
	for _, f := range files {
		buf, err := fs.ReadFile(fsys, f)
		if err != nil {
			t.Fatal(err)
		}
		var msgs map[string]any
		if err := yaml.Unmarshal(buf, &msgs); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, fam := range web.Families() {
			if v, ok := msgs[themeFamilyKey(fam)].(string); !ok || strings.TrimSpace(v) == "" {
				t.Errorf("%s: %s is missing or empty", f, themeFamilyKey(fam))
			}
		}
	}
}
