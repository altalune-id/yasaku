package templates

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func pagesAwaitingRedesign() []string {
	return []string{
		"onboarding.templ", "transaction_form.templ", "transactions.templ",
		"wallets.templ", "wallet_form.templ", "periods.templ",
		"reports.templ", "opensheet_yasaku.templ",
	}
}

func pagesUnmounted() []string {
	return []string{"categories.templ", "post_form.templ", "posts.templ", "tags.templ", "todos.templ", "todo_row.templ"}
}

var (
	rawCollapse  = regexp.MustCompile(`class="(?:[^"]*\s)?collapse(?:-arrow|-plus|-title|-content)?[\s"]`)
	rawLegend    = regexp.MustCompile(`<legend\b`)
	rawLabelFor  = regexp.MustCompile(`<label\b[^>]*\bfor="[^"]*"`)
	textControl  = regexp.MustCompile(`(?s)<(input|select|textarea)\b(.*?)>`)
	nonTextInput = regexp.MustCompile(`\btype="(hidden|checkbox|radio|submit|button)"`)
	formFieldAt  = regexp.MustCompile(`@FormField\(`)
	classedTag   = regexp.MustCompile(`(?s)<([a-zA-Z][\w-]*)\b([^<>]*?)\bclass=(?:"([^"]*)"|\{\s*"([^"]*)")`)
)

// TestPagesFollowTheKitAndThemeScale holds every page outside the redesign and unmounted lists to the form kit, semantic colors and the Tailwind 4 scale.
func TestPagesFollowTheKitAndThemeScale(t *testing.T) {
	for _, f := range guardedPageSources(t) {
		for _, v := range pageViolations(readTemplate(t, f)) {
			t.Errorf("%s: %s", f, v)
		}
	}
}

// TestRedesignAndUnmountedListsOnlyHoldRealLaggards fails when a listed page is gone or already passes every page guard, so the lists only shrink.
func TestRedesignAndUnmountedListsOnlyHoldRealLaggards(t *testing.T) {
	for _, f := range pagesAwaitingRedesign() {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s is listed in pagesAwaitingRedesign but does not exist; remove %s from the list", f, f)
			continue
		}
		if len(pageViolations(readTemplate(t, f))) == 0 {
			t.Errorf("%s passes every page guard; remove %s from pagesAwaitingRedesign", f, f)
		}
	}
	for _, f := range pagesUnmounted() {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s is listed in pagesUnmounted but does not exist; remove %s from the list", f, f)
		}
	}
}

// TestNoRawCollapseAndButtonsOnlyOnControls applies to every template: collapses come from AppCollapse and btn sits only on interactive elements.
func TestNoRawCollapseAndButtonsOnlyOnControls(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range files {
		f := e.Name()
		if !strings.HasSuffix(f, ".templ") {
			continue
		}
		src := readTemplate(t, f)
		if f != "ui_form.templ" {
			if m := rawCollapse.FindString(src); m != "" {
				t.Errorf("%s: raw collapse %q; use AppCollapse", f, m)
			}
		}
		for _, tag := range btnOnNonControls(src) {
			t.Errorf("%s: btn on a non-interactive <%s>; use a button, link or summary", f, tag)
		}
	}
}

func guardedPageSources(t *testing.T) []string {
	t.Helper()
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := []string{"flash.go"}
	for _, e := range files {
		f := e.Name()
		if strings.HasSuffix(f, ".templ") && f != "ui_form.templ" && !slices.Contains(pagesAwaitingRedesign(), f) && !slices.Contains(pagesUnmounted(), f) {
			out = append(out, f)
		}
	}
	if len(out) < 20 {
		t.Fatalf("guarded only %d sources; the listing is broken", len(out))
	}
	return out
}

func readTemplate(t *testing.T, f string) string {
	t.Helper()
	raw, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return comment.ReplaceAllString(string(raw), "")
}

func pageViolations(src string) []string {
	var out []string
	for line := range strings.SplitSeq(src, "\n") {
		if m := rawPalette.FindString(line); m != "" {
			out = append(out, "raw palette class "+m)
		}
		if m := largeControl.FindStringSubmatch(line); m != nil {
			out = append(out, m[2]+"-lg control")
		}
		if m := tw3Scale.FindStringSubmatch(line); m != nil {
			out = append(out, "Tailwind 3 scale "+m[2])
		}
	}
	if rawLegend.MatchString(src) {
		out = append(out, "raw <legend>; use FormSection or FieldGroup")
	}
	for _, m := range rawLabelFor.FindAllString(src, -1) {
		if !strings.Contains(m, `for="alt-drawer"`) {
			out = append(out, "raw labeled field "+m+"; use FormField")
		}
	}
	blocks := formFieldBlocks(src)
	for _, b := range blocks {
		if !strings.Contains(src[b[0]:b[1]], "Attrs()...") {
			out = append(out, "FormField block without Attrs() on its control: "+oneLine(src[b[0]:min(b[1], b[0]+80)]))
		}
	}
	for _, m := range textControl.FindAllStringSubmatch(src, -1) {
		if strings.Contains(m[2], "font-mono") && !strings.Contains(m[2], `dir="ltr"`) {
			out = append(out, "monospace ID or slug input without dir=\"ltr\" flips in RTL: "+oneLine(m[0][:min(len(m[0]), 80)]))
		}
	}
	for _, m := range textControl.FindAllStringSubmatchIndex(src, -1) {
		attrs := src[m[4]:m[5]]
		if nonTextInput.MatchString(attrs) || strings.Contains(attrs, "aria-label") || strings.Contains(attrs, "join-item") {
			continue
		}
		if !slices.ContainsFunc(blocks, func(r [2]int) bool { return m[0] > r[0] && m[0] < r[1] }) {
			out = append(out, "text control outside FormField: "+oneLine(src[m[0]:min(m[1], m[0]+80)]))
		}
	}
	return out
}

func formFieldBlocks(src string) [][2]int {
	var out [][2]int
	for _, start := range formFieldAt.FindAllStringIndex(src, -1) {
		open := strings.Index(src[start[1]:], "{\n")
		if open < 0 {
			continue
		}
		depth, i := 0, start[1]+open
		for ; i < len(src); i++ {
			switch src[i] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		out = append(out, [2]int{start[0], i})
	}
	return out
}

func btnOnNonControls(src string) []string {
	var out []string
	for _, m := range classedTag.FindAllStringSubmatch(src, -1) {
		cls := m[3] + m[4]
		if !slices.Contains(strings.Fields(cls), "btn") {
			continue
		}
		switch strings.ToLower(m[1]) {
		case "button", "a", "summary", "label", "input":
		default:
			out = append(out, m[1])
		}
	}
	return out
}
