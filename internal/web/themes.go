package web

import (
	_ "embed"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Color modes a person can pick; System renders no data-theme so the OS decides.
const (
	ModeLight  = "light"
	ModeDark   = "dark"
	ModeSystem = "system"
)

// ThemeCookieName is the cookie carrying a signed-out visitor's theme choice.
const ThemeCookieName = "alt_theme"

//go:embed ui/themes.css
var themeSource string

// Theme is one daisyUI theme block declared in internal/web/ui/themes.css.
type Theme struct {
	Name            string
	Family          string
	Mode            string
	Root            string
	Default         bool
	PrefersDark     bool
	ThemeController bool
	Tokens          map[string]string
}

var (
	themeBlock = regexp.MustCompile(`(?s)@plugin\s+"\./daisyui-theme\.mjs"\s*\{(.*?)\n\}`)
	themeDecl  = regexp.MustCompile(`(?m)^\s*([a-z0-9-]+):\s*(.+?);\s*$`)
	cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	oklchValue = regexp.MustCompile(`^oklch\(\s*([\d.]+)%\s+([\d.]+)\s+([\d.]+)\s*\)$`)
)

var themes = sync.OnceValues(func() ([]Theme, error) { //nolint:gochecknoglobals // parsed once from an embedded constant.
	return parseThemes(themeSource)
})

// LoadThemes parses the embedded theme source; boot calls it so a broken source stops startup.
func LoadThemes() error {
	_, err := themes()
	return err
}

// Themes returns every theme block parsed from the embedded theme source, in source order.
func Themes() []Theme {
	ts, err := themes()
	if err != nil {
		panic(err)
	}
	return ts
}

// Families returns the theme family names in source order, default first.
func Families() []string {
	var out []string
	for _, t := range Themes() {
		if !slices.Contains(out, t.Family) {
			out = append(out, t.Family)
		}
	}
	return out
}

// DefaultFamily returns the family whose default theme is scoped to the bare :root.
func DefaultFamily() string { return defaultFamily(Themes()) }

func defaultFamily(ts []Theme) string {
	for _, t := range ts {
		if t.Default && t.Root == ":root" {
			return t.Family
		}
	}
	return ""
}

func parseThemes(src string) ([]Theme, error) {
	var out []Theme
	for _, b := range themeBlock.FindAllStringSubmatch(cssComment.ReplaceAllString(src, ""), -1) {
		t := Theme{Root: ":root", ThemeController: true, Tokens: map[string]string{}}
		for _, d := range themeDecl.FindAllStringSubmatch(b[1], -1) {
			key, val := d[1], d[2]
			switch {
			case strings.HasPrefix(key, "--"):
				t.Tokens[key] = val
			case key == "name":
				t.Name = strings.Trim(val, `"`)
			case key == "root":
				t.Root = strings.Trim(val, `"`)
			case key == "themecontroller":
				t.ThemeController = val != "false"
			case key == "default":
				t.Default = val == "true"
			case key == "prefersdark":
				t.PrefersDark = val == "true"
			}
		}
		i := strings.LastIndex(t.Name, "-")
		if i <= 0 {
			return nil, fmt.Errorf("web: theme %q is not named <family>-<mode>", t.Name)
		}
		t.Family, t.Mode = t.Name[:i], t.Name[i+1:]
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, errors.New("web: no theme blocks in the theme source")
	}
	return out, validateRoots(out)
}

func validateRoots(ts []Theme) error {
	var defaults []string
	for _, t := range ts {
		if t.Default && t.Root == ":root" && !slices.Contains(defaults, t.Family) {
			defaults = append(defaults, t.Family)
		}
	}
	if len(defaults) != 1 {
		return fmt.Errorf("web: want exactly one family with its default on :root, got %v", defaults)
	}
	for _, t := range ts {
		want := ":root[data-family='" + t.Family + "']"
		if t.Family == defaults[0] {
			want = ":root"
		}
		if t.Root != want {
			return fmt.Errorf("web: theme %q has root %q, want %q", t.Name, t.Root, want)
		}
	}
	return nil
}

// ThemePref is a validated family and color mode.
type ThemePref struct {
	Family string
	Mode   string
}

// ParseThemePref allowlists family against the registry and mode against light, dark and system; anything else becomes the default.
func ParseThemePref(family, mode string) ThemePref {
	p := ThemePref{Family: DefaultFamily(), Mode: ModeSystem}
	if slices.Contains(Families(), family) {
		p.Family = family
	}
	if mode == ModeLight || mode == ModeDark {
		p.Mode = mode
	}
	return p
}

// ParseThemeCookie reads a CookieValue back; malformed or old values become the default.
func ParseThemeCookie(raw string) ThemePref {
	i := strings.LastIndex(raw, "-")
	if i < 0 {
		return ParseThemePref("", "")
	}
	return ParseThemePref(raw[:i], raw[i+1:])
}

// CookieValue encodes p as <family>-<mode>.
func (p ThemePref) CookieValue() string { return p.Family + "-" + p.Mode }

// Attr is the data-theme value to render on <html>; empty for System.
func (p ThemePref) Attr() string {
	if p.Mode == ModeSystem {
		return ""
	}
	return p.Family + "-" + p.Mode
}

// FamilyAttr is the data-family value to render on <html>: set only for System with a non-default family.
func (p ThemePref) FamilyAttr() string {
	if p.Mode != ModeSystem || p.Family == DefaultFamily() {
		return ""
	}
	return p.Family
}

// ThemeMeta is one theme-color meta tag; Media is empty for an unconditional one.
type ThemeMeta struct {
	Media string
	Color string
}

// Metas returns one theme-color meta for Light or Dark and a media-keyed pair for System.
func (p ThemePref) Metas() []ThemeMeta {
	if p.Mode != ModeSystem {
		return []ThemeMeta{{Color: themeColor(p.Family + "-" + p.Mode)}}
	}
	return []ThemeMeta{
		{Media: "(prefers-color-scheme: light)", Color: themeColor(p.Family + "-" + ModeLight)},
		{Media: "(prefers-color-scheme: dark)", Color: themeColor(p.Family + "-" + ModeDark)},
	}
}

func themeColor(name string) string {
	for _, t := range Themes() {
		if t.Name == name {
			hex, err := oklchHex(t.Tokens["--color-base-100"])
			if err == nil {
				return hex
			}
		}
	}
	return ""
}

func oklchHex(v string) (string, error) {
	rgb, err := oklchRGB(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("#%02x%02x%02x", byte(math.Round(rgb[0]*255)), byte(math.Round(rgb[1]*255)), byte(math.Round(rgb[2]*255))), nil
}

func parseOklch(v string) (l, c, h float64, err error) {
	m := oklchValue.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, 0, 0, fmt.Errorf("web: %q is not oklch(L%% C H)", v)
	}
	l, _ = strconv.ParseFloat(m[1], 64)
	c, _ = strconv.ParseFloat(m[2], 64)
	h, _ = strconv.ParseFloat(m[3], 64)
	return l / 100, c, h, nil
}

// NOTE: OKLab to linear sRGB per https://bottosson.github.io/posts/oklab/, not gamut-clipped.
func oklchLinear(l, c, h float64) [3]float64 {
	a, b := c*math.Cos(h*math.Pi/180), c*math.Sin(h*math.Pi/180)
	lc := l + 0.3963377774*a + 0.2158037573*b
	mc := l - 0.1055613458*a - 0.0638541728*b
	sc := l - 0.0894841775*a - 1.2914855480*b
	l3, m3, s3 := lc*lc*lc, mc*mc*mc, sc*sc*sc
	return [3]float64{
		4.0767416621*l3 - 3.3077115913*m3 + 0.2309699292*s3,
		-1.2684380046*l3 + 2.6097574011*m3 - 0.3413193965*s3,
		-0.0041960863*l3 - 0.7034186147*m3 + 1.7076147010*s3,
	}
}

func oklchRGB(v string) ([3]float64, error) {
	l, c, h, err := parseOklch(v)
	if err != nil {
		return [3]float64{}, err
	}
	var out [3]float64
	for i, x := range oklchLinear(l, c, h) {
		if x <= 0.0031308 {
			x *= 12.92
		} else {
			x = 1.055*math.Pow(x, 1/2.4) - 0.055
		}
		out[i] = min(1, max(0, x))
	}
	return out, nil
}
