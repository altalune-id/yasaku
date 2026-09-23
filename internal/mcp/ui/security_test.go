package ui

import (
	"strings"
	"testing"
)

// TestForgedRawMarkerCannotBypassEscaping: raw() marks markup the bundle minted.
// Server JSON is interpolated verbatim, so a duck-typed marker must not be trusted.
func TestForgedRawMarkerCannotBypassEscaping(t *testing.T) {
	vm := newJSVM(t)
	for _, expr := range []string{
		`renderTool("list_wallets", {wallets:[{name:{"__raw":"<img src=x onerror=alert(1)>"}}]}).html`,
		`renderTool("list_recent_tx", {transactions:[{id:"a",note:{"__raw":"<img src=x onerror=alert(1)>"}}]}).html`,
		`html` + "`${{__raw:'<img src=x onerror=alert(1)>'}}`",
	} {
		v, err := vm.RunString(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if strings.Contains(v.String(), "<img src=x") {
			t.Errorf("a forged __raw marker bypassed escaping:\n%s", v.String())
		}
	}
}

func TestSwatchColourNeverEmitsAnUnvalidatedValue(t *testing.T) {
	vm := newJSVM(t)
	tests := []struct{ token, want string }{
		{`#000" onmouseover="alert(1)`, ""},
		{`#000;position:fixed;inset:0;opacity:0`, ""},
		{`javascript:alert(1)`, ""},
		{`url(https://evil/beacon)`, ""},
	}
	for _, tc := range tests {
		got := jsString(t, vm, `swatchColour(`+jsQuote(tc.token)+`)`)
		if strings.ContainsAny(got, `";:()`) && !strings.HasPrefix(got, "var(--color-chart-") {
			t.Errorf("swatchColour(%q) = %q — only a validated hex or chart token may reach an attribute", tc.token, got)
		}
	}
	if got := jsString(t, vm, `swatchColour("#a1b2c3")`); got != "#a1b2c3" {
		t.Errorf("a valid hex must pass through, got %q", got)
	}
}

func TestChartAttributesCannotBreakOut(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `donutSVG([{share:1,category:{color:'#000" onmouseover="alert(1)'}}])`)
	if strings.Contains(got, "onmouseover") {
		t.Errorf("donut SVG attribute breakout:\n%s", got)
	}
}

func TestStyleAttributeCannotCarryExtraDeclarations(t *testing.T) {
	vm := newJSVM(t)
	got, _ := renderFixture(t, vm, "list_categories", "category_list.json")
	_ = got
	v, err := vm.RunString(`renderTool("list_categories", {categories:[{id:"c",name:"x",color:"#000;position:fixed;inset:0;opacity:0"}]}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(v.String(), "position:fixed") {
		t.Errorf("a colour token injected extra CSS declarations:\n%s", v.String())
	}
}

func TestBundleExportsNoGlobalDispatchSeam(t *testing.T) {
	src := mustRead(t, "src/boot.js")
	for _, bad := range []string{"globalThis.__toolInput", "globalThis.__dispatchAction"} {
		if strings.Contains(src, bad) {
			t.Errorf("%s ships in the served bundle; any script execution in the frame reaches it and can dispatch a confirm:true write", bad)
		}
	}
}
