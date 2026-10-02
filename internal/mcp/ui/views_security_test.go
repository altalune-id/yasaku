package ui

import (
	"strings"
	"testing"
)

func TestSwatchColourNeverEmitsAnUnvalidatedValue(t *testing.T) {
	vm := newJSVM(t)
	for _, token := range []string{
		`#000" onmouseover="alert(1)`,
		`#000;position:fixed;inset:0;opacity:0`,
		`javascript:alert(1)`,
		`url(https://evil/beacon)`,
		`chart-1;background:url(https://evil/b)`,
	} {
		got := jsString(t, vm, `swatchColour(`+jsQuote(token)+`)`)
		if strings.ContainsAny(got, `";:()`) && !strings.HasPrefix(got, "var(--color-chart-") {
			t.Errorf("swatchColour(%q) = %q — only a validated hex or chart token may reach an attribute", token, got)
		}
		if strings.Contains(got, "evil") || strings.Contains(got, "onmouseover") || strings.Contains(got, "position:fixed") {
			t.Errorf("swatchColour(%q) = %q leaked the attacker's payload", token, got)
		}
	}
	if got := jsString(t, vm, `swatchColour("#a1b2c3")`); got != "#a1b2c3" {
		t.Errorf("a valid hex must pass through, got %q", got)
	}
	if got := jsString(t, vm, `swatchColour("chart-2")`); !strings.HasPrefix(got, "var(--color-chart-2,") {
		t.Errorf("a valid chart token must resolve to its custom property, got %q", got)
	}
}

func TestCategorySwatchCannotCarryExtraDeclarations(t *testing.T) {
	vm := newViewsVM(t)
	for _, tc := range []struct{ tool, payload, expr string }{
		{"list_categories", `{categories:[{id:"c",name:"x",color:"#000;position:fixed;inset:0;opacity:0"}]}`, "rows[0].swatch"},
		{"period_report", `{spendByCategory:[{category:{name:"x",color:"#000;position:fixed"},share:0.5}]}`, "categories[0].swatch"},
		{"period_report", `{spendByCategory:[{category:{name:"x"},share:"1;position:fixed"}]}`, "categories[0].width"},
	} {
		got := jsString(t, vm, `renderTool(`+jsQuote(tc.tool)+`, `+tc.payload+`).model.`+tc.expr)
		if strings.Contains(got, "position") || strings.Contains(got, ";") {
			t.Errorf("%s %s injected extra CSS declarations: %q", tc.tool, tc.expr, got)
		}
	}
}

// TestForgedRawMarkerCannotReachAYasakuViewModel: a duck-typed {__raw:…} from a tool result must fall back to the declared placeholder.
func TestForgedRawMarkerCannotReachAYasakuViewModel(t *testing.T) {
	vm := newViewsVM(t)
	const forged = `{"__raw":"<img src=x onerror=alert(1)>"}`
	for _, tc := range []struct{ tool, payload string }{
		{"list_wallets", `{wallets:[{name:` + forged + `,kind:` + forged + `}]}`},
		{"list_recent_tx", `{transactions:[{id:"a",note:` + forged + `,kind:` + forged + `,date:` + forged + `}]}`},
		{"get_wallet", `{wallet:{name:` + forged + `},recent:[{note:` + forged + `}]}`},
		{"wallet_totals", `{wallets:[{wallet:{name:` + forged + `}}],period:{name:` + forged + `}}`},
		{"period_report", `{period:{name:` + forged + `,status:` + forged + `},spendByCategory:[{category:{name:` + forged + `}}]}`},
		{"cashflow_report", `{points:[{period:{name:` + forged + `}}]}`},
		{"list_categories", `{categories:[{name:` + forged + `,kind:` + forged + `}]}`},
		{"list_projects", `{projects:[{projectName:` + forged + `,orgName:` + forged + `}]}`},
		{"now", `{timezone:` + forged + `,today:` + forged + `}`},
		{"create_wallet", `{needs:{needs:[{field:` + forged + `,reason:` + forged + `,candidates:[` + forged + `]}]}}`},
		{"create_wallet", `{preview:{name:` + forged + `},warning:` + forged + `}`},
		{"record_batch", `{preview:[{errorCode:` + forged + `,error:` + forged + `}]}`},
		{"adjust_balance", `{warning:` + forged + `}`},
	} {
		got := modelJSON(t, vm, tc.tool, tc.payload)
		for _, bad := range []string{"__raw", "<img src=x", "[object Object]"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s: a forged __raw marker left %q in the view model:\n%s", tc.tool, bad, got)
			}
		}
	}
}

func TestTxSignIsNotFooledByPrototypeKeys(t *testing.T) {
	vm := newViewsVM(t)
	for _, kind := range []string{"constructor", "toString", "__proto__", "hasOwnProperty"} {
		got := jsString(t, vm, `renderTool("list_recent_tx", {transactions:[{id:"x",kind:`+jsQuote(kind)+`}]}).model.rows[0].sign`)
		if got != "•" {
			t.Errorf("kind %q resolved the glyph to %q, want the neutral fallback", kind, got)
		}
	}
}

func TestNeedFieldIsNotFooledByPrototypeKeys(t *testing.T) {
	vm := newViewsVM(t)
	for _, field := range []string{"constructor", "toString", "hasOwnProperty"} {
		got := jsString(t, vm, `renderTool("create_wallet", {needs:{needs:[{field:`+jsQuote(field)+`}]}}).model.fields[0].name`)
		if got != field {
			t.Errorf("need field %q mapped to %q, want itself", field, got)
		}
	}
}

func TestMutationModelsAreNotReachableThroughObjectPrototype(t *testing.T) {
	vm := newViewsVM(t)
	if got := jsString(t, vm, `String(Object.getPrototypeOf(MUTATION_MODELS))`); got != "null" {
		t.Errorf("MUTATION_MODELS prototype = %s, want null", got)
	}
}
