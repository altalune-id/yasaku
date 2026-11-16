package templates

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
)

var summaryClass = regexp.MustCompile(`<summary[^>]*class="([^"]*)"`)

func TestRailFooter_RowsShareOneRecipe(t *testing.T) {
	d := web.LayoutData{
		Principal:     &session.Principal{Email: "a@example.com"},
		SupportedLocs: []i18n.Locale{i18n.EnUS, i18n.ArSA},
	}
	var b strings.Builder
	if err := railFooter(d).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	rows := summaryClass.FindAllStringSubmatch(out, -1)
	if len(rows) != 3 {
		t.Fatalf("want 3 footer rows (appearance, language, user), got %d: %s", len(rows), out)
	}
	for _, r := range rows[1:] {
		if r[1] != rows[0][1] {
			t.Errorf("footer rows differ:\n%q\n%q", rows[0][1], r[1])
		}
	}
	for _, want := range []string{"min-h-11", "text-sm", "font-normal", "list-none", "[&amp;::-webkit-details-marker]:hidden"} {
		if !strings.Contains(rows[0][1], want) {
			t.Errorf("footer row lacks %s: %q", want, rows[0][1])
		}
	}
	if strings.Contains(out, "collapse-arrow") || strings.Contains(out, "collapse-title") {
		t.Error("appearance must use the footer row, not daisyUI's collapse title")
	}
	if n := strings.Count(out, `class="grid size-6 shrink-0 place-items-center`); n != 3 {
		t.Errorf("each footer row starts with the same 24px slot; got %d", n)
	}
	if n := strings.Count(out, "ms-auto size-4 shrink-0"); n != 3 {
		t.Errorf("each footer row ends with the same chevron; got %d", n)
	}
}
