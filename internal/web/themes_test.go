package web

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestThemes_EveryBlockDefinesEveryToken(t *testing.T) {
	daisyThemeTokens := []string{
		"--color-base-100", "--color-base-200", "--color-base-300", "--color-base-content",
		"--color-primary", "--color-primary-content", "--color-secondary", "--color-secondary-content",
		"--color-accent", "--color-accent-content", "--color-neutral", "--color-neutral-content",
		"--color-info", "--color-info-content", "--color-success", "--color-success-content",
		"--color-warning", "--color-warning-content", "--color-error", "--color-error-content",
		"--radius-selector", "--radius-field", "--radius-box",
		"--size-selector", "--size-field", "--border", "--depth", "--noise",
	}
	themes := Themes()
	if len(themes) == 0 {
		t.Fatal("no themes parsed from the theme source")
	}
	for _, th := range themes {
		for _, tok := range daisyThemeTokens {
			if th.Tokens[tok] == "" {
				t.Errorf("%s: missing %s", th.Name, tok)
			}
		}
	}
}

func TestThemes_OnePairPerFamily(t *testing.T) {
	modes := map[string][]string{}
	for _, th := range Themes() {
		if th.Name != th.Family+"-"+th.Mode {
			t.Errorf("%s: name is not <family>-<mode>", th.Name)
		}
		modes[th.Family] = append(modes[th.Family], th.Mode)
	}
	for family, got := range modes {
		slices.Sort(got)
		if !slices.Equal(got, []string{ModeDark, ModeLight}) {
			t.Errorf("family %s has modes %v, want exactly one light and one dark", family, got)
		}
	}
	var inOrder []string
	for _, th := range Themes() {
		if !slices.Contains(inOrder, th.Family) {
			inOrder = append(inOrder, th.Family)
		}
	}
	if !slices.Equal(Families(), inOrder) {
		t.Errorf("Families() = %v, want the families of Themes() in source order %v", Families(), inOrder)
	}
}

func TestThemes_OneDefaultLightAndOnePrefersDarkPerFamily(t *testing.T) {
	for _, f := range Families() {
		var defaults, dark []string
		for _, th := range Themes() {
			if th.Family != f {
				continue
			}
			if th.Default {
				defaults = append(defaults, th.Name)
			}
			if th.PrefersDark {
				dark = append(dark, th.Name)
			}
		}
		if !slices.Equal(defaults, []string{f + "-light"}) {
			t.Errorf("family %s: default blocks %v, want exactly %s-light", f, defaults, f)
		}
		if !slices.Equal(dark, []string{f + "-dark"}) {
			t.Errorf("family %s: prefersdark blocks %v, want exactly %s-dark", f, dark, f)
		}
	}
}

func TestThemes_RootScopesEachFamily(t *testing.T) {
	ts := Themes()
	def := DefaultFamily()
	if def != "yasaku" {
		t.Fatalf("DefaultFamily() = %q, want yasaku", def)
	}
	if ts[0].Family != def || ts[1].Family != def {
		t.Errorf("the default family's blocks must come first, got %s, %s", ts[0].Name, ts[1].Name)
	}
	for _, th := range ts {
		want := ":root[data-family='" + th.Family + "']"
		if th.Family == def {
			want = ":root"
		}
		if th.Root != want {
			t.Errorf("%s: root %q, want %q", th.Name, th.Root, want)
		}
		if th.ThemeController {
			t.Errorf("%s: themecontroller must be false", th.Name)
		}
	}
	if got := Families(); len(got) == 0 || got[0] != def {
		t.Errorf("Families() = %v, want the default family first", got)
	}
	if got := Families(); !slices.Equal(got, []string{"yasaku", "glacier", "forest"}) {
		t.Errorf("Families() = %v", got)
	}
}

func TestCompiledCSS_RootScopedSystemSelectors(t *testing.T) {
	b, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	for _, want := range []string{
		":where(:root)",
		":root:not([data-theme])",
		`[data-theme="yasaku-dark"]`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css lacks %s", want)
		}
	}
	for _, f := range Families() {
		if f == DefaultFamily() {
			continue
		}
		for _, want := range []string{
			":where(:root[data-family='" + f + "'])",
			":root[data-family='" + f + "']:not([data-theme])",
			`[data-theme="` + f + `-light"]`,
			`[data-theme="` + f + `-dark"]`,
		} {
			if !strings.Contains(css, want) {
				t.Errorf("app.css lacks %s", want)
			}
		}
		if strings.Index(css, ":where(:root[data-family='"+f+"'])") < strings.Index(css, ":where(:root)") {
			t.Errorf("%s's zero-specificity default precedes the default family's, so it can never win", f)
		}
	}
	if strings.Contains(css, "input.theme-controller") {
		t.Error("app.css still emits theme-controller selectors")
	}
}

func TestThemes_RegistryEqualsCompiledCSS(t *testing.T) {
	css, err := os.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, m := range regexp.MustCompile(`\[data-theme="([a-z0-9-]+)"\]`).FindAllStringSubmatch(string(css), -1) {
		if !slices.Contains(want, m[1]) {
			want = append(want, m[1])
		}
	}
	got := names(Themes())
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("registry %v, compiled app.css themes %v", got, want)
	}
}

func TestThemeSource_NotServed(t *testing.T) {
	if _, err := StaticFS().Open("themes.css"); err == nil {
		b, _ := os.ReadFile("static/themes.css")
		if strings.Contains(string(b), "daisyui-theme.mjs") {
			t.Fatal("the theme source is reachable under /static")
		}
	}
	if _, err := StaticFS().Open("ui/themes.css"); err == nil {
		t.Fatal("ui/themes.css is reachable under /static")
	}
}

func TestParseThemePref_Allowlist(t *testing.T) {
	def := ThemePref{Family: DefaultFamily(), Mode: ModeSystem}
	for _, tc := range []struct {
		family, mode string
		want         ThemePref
	}{
		{DefaultFamily(), ModeLight, ThemePref{DefaultFamily(), ModeLight}},
		{DefaultFamily(), ModeDark, ThemePref{DefaultFamily(), ModeDark}},
		{DefaultFamily(), ModeSystem, def},
		{"", "", def},
		{"forest", ModeDark, ThemePref{"forest", ModeDark}},
		{"Forest", ModeDark, ThemePref{DefaultFamily(), ModeDark}},
		{DefaultFamily(), "auto", def},
		{DefaultFamily(), "DARK", def},
		{`yasaku"><script>`, `dark"><x`, def},
		{"glacier", "midnight", ThemePref{"glacier", ModeSystem}},
		{"midnight", "midnight", def},
	} {
		if got := ParseThemePref(tc.family, tc.mode); got != tc.want {
			t.Errorf("ParseThemePref(%q, %q) = %+v, want %+v", tc.family, tc.mode, got, tc.want)
		}
	}
}

func TestThemePref_AttrAndCookie(t *testing.T) {
	f := DefaultFamily()
	for _, tc := range []struct {
		pref ThemePref
		attr string
	}{
		{ThemePref{f, ModeLight}, f + "-light"},
		{ThemePref{f, ModeDark}, f + "-dark"},
		{ThemePref{f, ModeSystem}, ""},
	} {
		if got := tc.pref.Attr(); got != tc.attr {
			t.Errorf("%+v.Attr() = %q, want %q", tc.pref, got, tc.attr)
		}
		if got := tc.pref.FamilyAttr(); got != "" {
			t.Errorf("%+v.FamilyAttr() = %q, want empty for the default family", tc.pref, got)
		}
		if got := ParseThemeCookie(tc.pref.CookieValue()); got != tc.pref {
			t.Errorf("cookie round trip of %+v = %+v", tc.pref, got)
		}
	}
	def := ThemePref{f, ModeSystem}
	for _, raw := range []string{"", "slate", "dark", "auto", "yasaku", "midnight-dark-x", f + "-sepia", f + "-system-", "%00"} {
		if got := ParseThemeCookie(raw); got != def {
			t.Errorf("ParseThemeCookie(%q) = %+v, want default", raw, got)
		}
	}
	if got := ParseThemeCookie("midnight-dark"); got != (ThemePref{f, ModeDark}) {
		t.Errorf("an unknown family keeps a valid mode: got %+v", got)
	}
}

func TestThemePref_NonDefaultFamilyAttrs(t *testing.T) {
	for _, tc := range []struct {
		pref         ThemePref
		attr, family string
	}{
		{ThemePref{"forest", ModeLight}, "forest-light", ""},
		{ThemePref{"forest", ModeDark}, "forest-dark", ""},
		{ThemePref{"forest", ModeSystem}, "", "forest"},
	} {
		if got := tc.pref.Attr(); got != tc.attr {
			t.Errorf("%+v.Attr() = %q, want %q", tc.pref, got, tc.attr)
		}
		if got := tc.pref.FamilyAttr(); got != tc.family {
			t.Errorf("%+v.FamilyAttr() = %q, want %q", tc.pref, got, tc.family)
		}
		if got := ParseThemeCookie(tc.pref.CookieValue()); got != tc.pref {
			t.Errorf("cookie round trip of %+v = %+v", tc.pref, got)
		}
	}
}

func TestThemePref_Metas(t *testing.T) {
	for _, f := range Families() {
		assertMetas(t, f)
	}
}

func assertMetas(t *testing.T, f string) {
	t.Helper()
	light, dark := themeByName(t, f+"-light"), themeByName(t, f+"-dark")
	lightHex, err := oklchHex(light.Tokens["--color-base-100"])
	if err != nil {
		t.Fatal(err)
	}
	darkHex, err := oklchHex(dark.Tokens["--color-base-100"])
	if err != nil {
		t.Fatal(err)
	}
	if got := (ThemePref{f, ModeLight}).Metas(); !slices.Equal(got, []ThemeMeta{{Color: lightHex}}) {
		t.Errorf("light metas = %+v", got)
	}
	if got := (ThemePref{f, ModeDark}).Metas(); !slices.Equal(got, []ThemeMeta{{Color: darkHex}}) {
		t.Errorf("dark metas = %+v", got)
	}
	want := []ThemeMeta{
		{Media: "(prefers-color-scheme: light)", Color: lightHex},
		{Media: "(prefers-color-scheme: dark)", Color: darkHex},
	}
	if got := (ThemePref{f, ModeSystem}).Metas(); !slices.Equal(got, want) {
		t.Errorf("system metas = %+v, want %+v", got, want)
	}
	if !regexp.MustCompile(`^#[0-9a-f]{6}$`).MatchString(lightHex) {
		t.Errorf("light hex %q", lightHex)
	}
}

func TestOklchHex_KnownValues(t *testing.T) {
	for in, want := range map[string]string{
		"oklch(100% 0 0)":           "#ffffff",
		"oklch(0% 0 0)":             "#000000",
		"oklch(62.8% 0.2577 29.23)": "#ff0000",
	} {
		got, err := oklchHex(in)
		if err != nil || got != want {
			t.Errorf("oklchHex(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := oklchHex("red"); err == nil {
		t.Error("oklchHex accepted a non-oklch value")
	}
}

var overrideAlpha = regexp.MustCompile(`var\(--color-base-content\)\s+(\d+)%`)

func renderedAlpha(t *testing.T, css, selectorStart string, daisyDefault float64) float64 {
	t.Helper()
	i := strings.Index(css, selectorStart)
	if i < 0 {
		return daisyDefault
	}
	body := css[i:]
	body = body[strings.Index(body, "{"):strings.Index(body, "}")]
	m := overrideAlpha.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("override %q sets no base-content mix", selectorStart)
	}
	if strings.Contains(body, "opacity") && !strings.Contains(body, "opacity: 1;") {
		t.Fatalf("override %q changes opacity; the contrast check assumes 1", selectorStart)
	}
	p, _ := strconv.Atoi(m[1])
	return float64(p) / 100
}

func TestThemes_ContrastAA(t *testing.T) {
	src, err := os.ReadFile("ui/app.tailwind.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(src)
	muted := []struct {
		part  string
		alpha float64
	}{
		{"inactive tab", renderedAlpha(t, css, ".tabs > .tab:not(", 0.5)},
		{"fieldset-legend", renderedAlpha(t, css, ".fieldset-legend", 1)},
		{"menu-title", renderedAlpha(t, css, ".menu-title", 0.4)},
		{"placeholder", renderedAlpha(t, css, "::placeholder", 0.5)},
	}
	for _, th := range Themes() {
		c := func(tok string) [3]float64 {
			rgb, err := oklchRGB(th.Tokens["--color-"+tok])
			if err != nil {
				t.Fatalf("%s: %s: %v", th.Name, tok, err)
			}
			return rgb
		}
		check := func(label string, fg, bg [3]float64, alpha float64) {
			if r := contrastRatio(fg, bg, alpha); r < 4.5 {
				t.Errorf("%s: %s %.2f:1, want >= 4.5:1", th.Name, label, r)
			}
		}
		for _, bg := range []string{"base-100", "base-200", "base-300"} {
			check("base-content on "+bg, c("base-content"), c(bg), 1)
			for _, m := range muted {
				check(m.part+" on "+bg, c("base-content"), c(bg), m.alpha)
			}
		}
		for _, k := range []string{"primary", "secondary", "accent", "neutral", "info", "success", "warning", "error"} {
			check(k+"-content on "+k, c(k+"-content"), c(k), 1)
		}
	}
}

func TestContrastRatio_Bounds(t *testing.T) {
	if r := contrastRatio([3]float64{0, 0, 0}, [3]float64{1, 1, 1}, 1); math.Abs(r-21) > 0.01 {
		t.Fatalf("black on white = %.3f, want 21", r)
	}
}

func contrastRatio(fg, bg [3]float64, alpha float64) float64 {
	var mixed [3]float64
	for i := range mixed {
		mixed[i] = alpha*fg[i] + (1-alpha)*bg[i]
	}
	x, y := relLuminance(mixed), relLuminance(bg)
	return (max(x, y) + 0.05) / (min(x, y) + 0.05)
}

func relLuminance(rgb [3]float64) float64 {
	var lin [3]float64
	for i, c := range rgb {
		if c <= 0.04045 {
			lin[i] = c / 12.92
			continue
		}
		lin[i] = math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin[0] + 0.7152*lin[1] + 0.0722*lin[2]
}

func names(ts []Theme) []string {
	out := make([]string, 0, len(ts))
	for _, th := range ts {
		out = append(out, th.Name)
	}
	return out
}

func themeByName(t *testing.T, name string) Theme {
	t.Helper()
	for _, th := range Themes() {
		if th.Name == name {
			return th
		}
	}
	t.Fatalf("no theme %s", name)
	return Theme{}
}

func TestParseThemes_RootAndControllerOptions(t *testing.T) {
	src := `@plugin "./daisyui-theme.mjs" {
  name: "moss-light";
  root: ":root[data-family='moss']";
  default: true;
  themecontroller: true;
  --color-base-100: oklch(99% 0 0);
}

@plugin "./daisyui-theme.mjs" {
  name: "ink-light";
  default: true;
  themecontroller: false;
  --color-base-100: oklch(99% 0 0);
}
`
	ts, err := parseThemes(src)
	if err != nil {
		t.Fatal(err)
	}
	if ts[0].Root != ":root[data-family='moss']" || !ts[0].ThemeController {
		t.Errorf("moss-light parsed as %+v", ts[0])
	}
	if ts[1].Root != ":root" || ts[1].ThemeController {
		t.Errorf("ink-light parsed as %+v", ts[1])
	}
	if got := defaultFamily(ts); got != "ink" {
		t.Errorf("defaultFamily = %q, want the family whose default sits on the bare :root", got)
	}
	if _, err := parseThemes(`@plugin "./daisyui-theme.mjs" {
  name: "nomode";
}
`); err == nil {
		t.Error("a block not named <family>-<mode> was accepted")
	}
}

var layerToken = regexp.MustCompile(`@layer\s+([\w.-]+)\s*\{|[{}]`)

func layerPathAt(css string, at int) []string {
	var stack []string
	for _, m := range layerToken.FindAllStringSubmatchIndex(css[:at], -1) {
		switch {
		case m[2] >= 0:
			stack = append(stack, css[m[2]:m[3]])
		case css[m[0]] == '{':
			stack = append(stack, "")
		default:
			stack = stack[:len(stack)-1]
		}
	}
	var out []string
	for _, s := range stack {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func TestCompiledCSS_ContrastOverridesSitBetweenDaisyUIAndUtilities(t *testing.T) {
	b, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	daisy := strings.Index(css, "@layer daisyui")
	contrast := strings.Index(css, "@layer contrast")
	if daisy < 0 || contrast < 0 || contrast < daisy {
		t.Fatalf("want the contrast sub-layer declared after daisyUI's (daisyui at %d, contrast at %d)", daisy, contrast)
	}
	if got := layerPathAt(css, daisy); !slices.Equal(got, []string{"utilities"}) {
		t.Fatalf("daisyUI components sit in %v, want inside utilities", got)
	}
	for _, sel := range []string{".tabs > .tab:not(", ".menu-title {", ":not(:disabled)::placeholder"} {
		i := strings.Index(css, sel)
		for i >= 0 && !slices.Equal(layerPathAt(css, i), []string{"utilities", "contrast"}) {
			next := strings.Index(css[i+1:], sel)
			if next < 0 {
				i = -1
				break
			}
			i += next + 1
		}
		if i < 0 {
			t.Errorf("no %q rule inside @layer utilities > contrast", sel)
		}
	}
	for _, m := range regexp.MustCompile(`(?m)^(\S[^{\n]*)\{`).FindAllStringSubmatch(css, -1) {
		if strings.Contains(m[1], "placeholder") || strings.Contains(m[1], "menu-title") {
			t.Errorf("unlayered rule %q beats every utility", m[1])
		}
	}
}

func TestParseThemes_RejectsWrongRoot(t *testing.T) {
	block := func(name, root string, def bool) string {
		out := "@plugin \"./daisyui-theme.mjs\" {\n  name: \"" + name + "\";\n"
		if root != "" {
			out += "  root: \"" + root + "\";\n"
		}
		if def {
			out += "  default: true;\n"
		}
		return out + "}\n\n"
	}
	good := block("ink-light", ":root", true) + block("ink-dark", ":root", false) +
		block("moss-light", ":root[data-family='moss']", true) + block("moss-dark", ":root[data-family='moss']", false)
	if _, err := parseThemes(good); err != nil {
		t.Fatalf("valid source rejected: %v", err)
	}
	for name, src := range map[string]string{
		"other family on bare root":     block("ink-light", ":root", true) + block("ink-dark", ":root", false) + block("moss-light", "", true) + block("moss-dark", ":root[data-family='moss']", false),
		"root names another family":     block("ink-light", ":root", true) + block("ink-dark", ":root", false) + block("moss-light", ":root[data-family='ink']", true) + block("moss-dark", ":root[data-family='moss']", false),
		"double-quoted family selector": block("ink-light", ":root", true) + block("ink-dark", ":root", false) + block("moss-light", `:root[data-family=moss]`, true) + block("moss-dark", ":root[data-family='moss']", false),
		"default family dark scoped":    block("ink-light", ":root", true) + block("ink-dark", ":root[data-family='ink']", false),
		"no bare-root default":          block("moss-light", ":root[data-family='moss']", true) + block("moss-dark", ":root[data-family='moss']", false),
		"two bare-root defaults":        block("ink-light", ":root", true) + block("ink-dark", ":root", false) + block("moss-light", ":root", true) + block("moss-dark", ":root", false),
	} {
		if _, err := parseThemes(src); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestThemes_BrandPerModeAndContrast(t *testing.T) {
	want := map[string]string{ModeLight: "#a86f00", ModeDark: "#ffbb00"}
	for _, th := range Themes() {
		got := th.Tokens["--brand"]
		if got != want[th.Mode] {
			t.Errorf("%s: --brand = %q, want %q", th.Name, got, want[th.Mode])
			continue
		}
		bg, err := oklchRGB(th.Tokens["--color-base-100"])
		if err != nil {
			t.Fatalf("%s base-100: %v", th.Name, err)
		}
		r := contrastRatio(hexRGB(t, got), bg, 1)
		t.Logf("%s: brand %s on base-100 = %.2f:1", th.Name, got, r)
		if r < 3 {
			t.Errorf("%s: brand %s on base-100 = %.2f:1, want at least 3:1 for graphics", th.Name, got, r)
		}
	}
}

func hexRGB(t *testing.T, hex string) [3]float64 {
	t.Helper()
	var rgb [3]float64
	for i := range rgb {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("bad hex %q: %v", hex, err)
		}
		rgb[i] = float64(v) / 255
	}
	return rgb
}

func formulaTokens() []string {
	return []string{"base-100", "base-200", "base-300", "base-content", "primary", "primary-content", "secondary", "secondary-content", "neutral", "neutral-content"}
}

const minPrimaryDistance = 0.07

func gamutChroma(l, h float64) float64 {
	lo, hi := 0.0, 0.4
	for range 40 {
		mid := (lo + hi) / 2
		in := true
		for _, x := range oklchLinear(l, mid, h) {
			in = in && x >= -1e-4 && x <= 1+1e-4
		}
		if in {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}

func formulaViolations(th, ref Theme) []string {
	_, _, hue, err := parseOklch(th.Tokens["--color-primary"])
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	for key, want := range ref.Tokens {
		got := th.Tokens[key]
		if !slices.Contains(formulaTokens(), strings.TrimPrefix(key, "--color-")) {
			if got != want {
				out = append(out, fmt.Sprintf("%s = %q, want %q", key, got, want))
			}
			continue
		}
		rl, rc, _, _ := parseOklch(want)
		l, c, h, err := parseOklch(got)
		if err != nil {
			out = append(out, err.Error())
			continue
		}
		wc := min(rc, gamutChroma(rl, hue))
		if l != rl || h != hue || c > wc+0.0005 || c < wc-0.002 {
			out = append(out, fmt.Sprintf("%s = %s, want oklch(%.4g%% %.3f %g)", key, got, rl*100, wc, hue))
		}
	}
	slices.Sort(out)
	return out
}

func TestThemes_FamiliesFollowFormula(t *testing.T) {
	def := DefaultFamily()
	ref := map[string]Theme{}
	for _, th := range Themes() {
		if th.Family == def {
			ref[th.Mode] = th
		}
	}
	for _, th := range Themes() {
		if th.Family == def {
			continue
		}
		for _, v := range formulaViolations(th, ref[th.Mode]) {
			t.Errorf("%s: %s", th.Name, v)
		}
	}
}

func TestFormulaViolations_CatchOffFormulaTokens(t *testing.T) {
	ref := themeByName(t, DefaultFamily()+"-light")
	derive := func(edit func(map[string]string)) Theme {
		th := Theme{Tokens: map[string]string{}}
		for k, v := range ref.Tokens {
			th.Tokens[k] = v
			if slices.Contains(formulaTokens(), strings.TrimPrefix(k, "--color-")) {
				l, c, _, _ := parseOklch(v)
				th.Tokens[k] = fmt.Sprintf("oklch(%g%% %.3f 200)", l*100, math.Floor(min(c, gamutChroma(l, 200))*1000)/1000)
			}
		}
		edit(th.Tokens)
		return th
	}
	if v := formulaViolations(derive(func(map[string]string) {}), ref); len(v) != 0 {
		t.Fatalf("a formula-derived family was rejected: %v", v)
	}
	for name, edit := range map[string]func(map[string]string){
		"base lightness off":    func(m map[string]string) { m["--color-base-100"] = "oklch(97% 0.004 200)" },
		"token on another hue":  func(m map[string]string) { m["--color-neutral"] = "oklch(28% 0.035 210)" },
		"chroma past the gamut": func(m map[string]string) { m["--color-primary"] = "oklch(48% 0.14 200)" },
		"chroma muted":          func(m map[string]string) { m["--color-secondary"] = "oklch(42% 0.03 200)" },
		"fixed accent changed":  func(m map[string]string) { m["--color-accent"] = "oklch(62% 0.13 200)" },
		"fixed radius changed":  func(m map[string]string) { m["--radius-box"] = "0.5rem" },
	} {
		if v := formulaViolations(derive(edit), ref); len(v) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}

func oklabDistance(t *testing.T, x, y string) float64 {
	t.Helper()
	lab := func(v string) [3]float64 {
		l, c, h, err := parseOklch(v)
		if err != nil {
			t.Fatal(err)
		}
		return [3]float64{l, c * math.Cos(h*math.Pi/180), c * math.Sin(h*math.Pi/180)}
	}
	a, b := lab(x), lab(y)
	return math.Sqrt((a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) + (a[2]-b[2])*(a[2]-b[2]))
}

func TestThemes_PrimaryClearOfSemanticColors(t *testing.T) {
	for _, th := range Themes() {
		for _, k := range []string{"info", "success", "warning", "error", "accent"} {
			if d := oklabDistance(t, th.Tokens["--color-primary"], th.Tokens["--color-"+k]); d < minPrimaryDistance {
				t.Errorf("%s: primary is %.3f OKLab from %s, want >= %.2f", th.Name, d, k, minPrimaryDistance)
			}
		}
	}
}

func TestParseThemes_SkipsCommentedBlocks(t *testing.T) {
	src := `@plugin "./daisyui-theme.mjs" {
  name: "ink-light";
  default: true;
}

/* TODO: hidden.
@plugin "./daisyui-theme.mjs" {
  name: "moss-light";
  root: ":root[data-family='moss']";
  default: true;
}
*/
`
	ts, err := parseThemes(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ts); !slices.Equal(got, []string{"ink-light"}) {
		t.Errorf("parsed %v, want only ink-light", got)
	}
}

func TestThemes_DarkNeutralStaysDark(t *testing.T) {
	for _, th := range Themes() {
		if th.Mode != ModeDark {
			continue
		}
		l, _, _, err := parseOklch(th.Tokens["--color-neutral"])
		if err != nil {
			t.Fatalf("%s neutral: %v", th.Name, err)
		}
		if l > 0.45 {
			t.Errorf("%s: neutral lightness %.2f; active menu rows and tips must stay dark on a dark theme", th.Name, l)
		}
	}
}
