package web

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/platform/session"
)

var tipToken = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func TestTipRegistry_IDsAreUniqueAndFlowsHoldAtMostThree(t *testing.T) {
	seen := map[string]bool{}
	perFlow := map[string]int{}
	for _, tip := range Tips() {
		if seen[tip.ID] {
			t.Errorf("duplicate tip id %q", tip.ID)
		}
		seen[tip.ID] = true
		perFlow[tip.Flow]++
		for _, tok := range []string{tip.ID, tip.Flow} {
			if !tipToken.MatchString(tok) {
				t.Errorf("tip %q: %q is not a lowercase token", tip.ID, tok)
			}
		}
		if tip.Target != "" && !tipToken.MatchString(tip.Target) {
			t.Errorf("tip %q: target %q is not a lowercase token", tip.ID, tip.Target)
		}
	}
	if len(seen) == 0 {
		t.Fatal("the registry is empty")
	}
	for flow, n := range perFlow {
		if n > MaxTipsPerFlow {
			t.Errorf("flow %q has %d tips, at most %d allowed", flow, n, MaxTipsPerFlow)
		}
	}
}

func TestTipRegistry_TextIsATranslationKey(t *testing.T) {
	for _, tip := range Tips() {
		if tip.TextKey != "tips."+strings.ReplaceAll(tip.ID, "-", "_") {
			t.Errorf("tip %q: text key %q must be tips.<id with underscores>", tip.ID, tip.TextKey)
		}
	}
}

func TestLookupTip_OnlyRegisteredIDs(t *testing.T) {
	first := Tips()[0]
	if got, ok := LookupTip(first.ID); !ok || got != first {
		t.Fatalf("LookupTip(%q) = %v, %v", first.ID, got, ok)
	}
	for _, bad := range []string{"", "nope", strings.ToUpper(first.ID), first.ID + " ", "../" + first.ID} {
		if _, ok := LookupTip(bad); ok {
			t.Errorf("LookupTip(%q) accepted an unregistered id", bad)
		}
	}
}

func TestTip_FlowIDsListsTheWholeFlow(t *testing.T) {
	first := Tips()[0]
	ids := first.FlowIDs()
	want := 0
	for _, tip := range Tips() {
		if tip.Flow == first.Flow {
			want++
		}
	}
	if len(ids) != want {
		t.Fatalf("FlowIDs() = %v, want %d ids", ids, want)
	}
	for _, id := range ids {
		tip, _ := LookupTip(id)
		if tip.Flow != first.Flow {
			t.Errorf("FlowIDs() includes %q from flow %q", id, tip.Flow)
		}
	}
}

func TestLayoutData_TipSeenReadsThePrincipal(t *testing.T) {
	if (LayoutData{}).TipSeen("a") {
		t.Error("signed out: nothing is seen")
	}
	d := LayoutData{Principal: &session.Principal{TipsSeen: []string{"a"}}}
	if !d.TipSeen("a") || d.TipSeen("b") {
		t.Errorf("TipSeen wrong for %v", d.Principal.TipsSeen)
	}
}

var tipsScriptBanned = regexp.MustCompile(`\b(textContent|innerText|outerText|innerHTML|outerHTML|insertAdjacentHTML|insertAdjacentText|createContextualFragment|DOMParser|setHTMLUnsafe)\b|document\s*\.\s*write|\beval\s*\(`)

func TestTipsScript_WritesNoMarkup(t *testing.T) {
	b, err := os.ReadFile("static/tips.js")
	if err != nil {
		t.Fatal(err)
	}
	if hit := tipsScriptBanned.FindAllString(string(b), -1); len(hit) > 0 {
		t.Errorf("tips.js uses %v: tip text is server-rendered from translation keys only", hit)
	}
}

func TestTipMechanismHoldsNoAppData(t *testing.T) {
	b, err := os.ReadFile("tips.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, tip := range Tips() {
		if strings.Contains(string(b), `"`+tip.ID+`"`) || strings.Contains(string(b), `"`+tip.TextKey+`"`) {
			t.Errorf("tips.go names tip %q: the app's list belongs in its own file", tip.ID)
		}
	}
}
