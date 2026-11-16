package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
)

func tipFixture(t *testing.T, anchored bool) web.Tip {
	t.Helper()
	for _, tip := range web.Tips() {
		if (tip.Target != "") == anchored {
			return tip
		}
	}
	t.Fatalf("no tip with anchored=%v", anchored)
	return web.Tip{}
}

func tipData(seen ...string) web.LayoutData {
	return web.LayoutData{Nonce: "n0nce", CurrentPath: "/p/x?y=1", Principal: &session.Principal{Email: "a@example.com", TipsSeen: seen}}
}

func TestTip_TextComesFromItsTranslationKey(t *testing.T) {
	for _, tip := range web.Tips() {
		out := render(t, Tip(tipData(), tip.ID))
		if !strings.Contains(out, ">"+tip.TextKey+"<") {
			t.Errorf("Tip(%q) does not render d.Tr(%q): %s", tip.ID, tip.TextKey, out)
		}
	}
}

func TestTip_RendersNothingForUnknownOrSeenOrSignedOut(t *testing.T) {
	tip := tipFixture(t, false)
	for name, out := range map[string]string{
		"unknown":    render(t, Tip(tipData(), "nope")),
		"seen":       render(t, Tip(tipData(tip.ID), tip.ID)),
		"signed out": render(t, Tip(web.LayoutData{Nonce: "n0nce"}, tip.ID)),
	} {
		if strings.TrimSpace(out) != "" {
			t.Errorf("%s: want no markup, got %s", name, out)
		}
	}
}

func TestTip_DismissPostsWithAndWithoutJS(t *testing.T) {
	tip := tipFixture(t, false)
	out := render(t, Tip(tipData(), tip.ID))
	for _, want := range []string{
		`data-tip="` + tip.ID + `"`,
		`data-tip-flow="` + tip.Flow + `"`,
		`role="note"`,
		`tabindex="-1"`,
		`method="post"`,
		`action="/tips/` + tip.ID + `/seen"`,
		`hx-post="/tips/` + tip.ID + `/seen"`,
		`hx-nonce="n0nce"`,
		`hx-swap="none"`,
		`name="redirect" value="/p/x?y=1"`,
		`name="all" value="1"`,
		`>tips.skip_all<`,
		`>tips.got_it<`,
		`class="alert `,
		` outline-none `,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("inline tip lacks %s\n%s", want, out)
		}
	}
	if open := out[:strings.Index(out, ">")]; strings.Contains(open, "data-tip-for") || strings.Contains(open, " hidden") {
		t.Errorf("an inline tip has no anchor and shows without JS: %s", out)
	}
}

func TestTip_AnchoredTipWaitsForItsTarget(t *testing.T) {
	tip := tipFixture(t, true)
	out := render(t, Tip(tipData(), tip.ID))
	open := out[:strings.Index(out, ">")]
	for _, want := range []string{`data-tip-for="` + tip.Target + `"`, ` hidden`, `class="card `, ` z-20 `, ` outline-none `} {
		if !strings.Contains(open, want) {
			t.Errorf("anchored tip lacks %s\n%s", want, out)
		}
	}
}
