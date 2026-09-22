package ui

import (
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// newJSVM loads every script part except bridge.js and boot.js, which need the
// ext-apps module and a DOM that goja does not provide.
func newJSVM(t *testing.T) *goja.Runtime {
	t.Helper()
	vm := goja.New()
	for _, p := range scriptParts {
		if p == "src/bridge.js" || p == "src/boot.js" {
			continue
		}
		if _, err := vm.RunString(mustRead(t, p)); err != nil {
			t.Fatalf("eval %s: %v", p, err)
		}
	}
	return vm
}

func jsString(t *testing.T, vm *goja.Runtime, expr string) string {
	t.Helper()
	v, err := vm.RunString(expr)
	if err != nil {
		t.Fatalf("eval %q: %v", expr, err)
	}
	return v.String()
}

func TestHTMLEscapesInterpolations(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, "html`<p>${'<script>alert(1)</script>'}</p>`")
	if got != "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>" {
		t.Errorf("html`` = %q, want the script tag escaped", got)
	}
}

func TestHTMLDoesNotEscapeLiteralMarkup(t *testing.T) {
	vm := newJSVM(t)
	if got := jsString(t, vm, "html`<b>${'x'}</b>`"); got != "<b>x</b>" {
		t.Errorf("html`` = %q", got)
	}
}

func TestHTMLJoinsArrays(t *testing.T) {
	vm := newJSVM(t)
	if got := jsString(t, vm, "html`${['<a>','b']}`"); got != "&lt;a&gt;b" {
		t.Errorf("html`` array = %q, want each item escaped and joined", got)
	}
}

func TestRawBypassesEscapingOnlyWhenAsked(t *testing.T) {
	vm := newJSVM(t)
	if got := jsString(t, vm, "html`${raw('<b>x</b>')}`"); got != "<b>x</b>" {
		t.Errorf("raw() = %q, want the markup passed through", got)
	}
	if got := jsString(t, vm, "html`${'<b>x</b>'}`"); got != "&lt;b&gt;x&lt;/b&gt;" {
		t.Errorf("a plain string = %q, want it escaped", got)
	}
	if got := jsString(t, vm, "html`${[raw('<i>a</i>'), '<i>b</i>']}`"); got != "<i>a</i>&lt;i&gt;b&lt;/i&gt;" {
		t.Errorf("mixed array = %q, want raw passed through and the string escaped", got)
	}
	if got := jsString(t, vm, "html`${raw(raw('<b>x</b>'))}`"); got != "<b>x</b>" {
		t.Errorf("raw(raw(x)) = %q — double-wrapping must be idempotent, not [object Object]", got)
	}
}

func TestFormatNumGroupsWithoutToLocaleString(t *testing.T) {
	vm := newJSVM(t)
	for expr, want := range map[string]string{
		`num(42)`:        "42",
		`num(1234567)`:   "1,234,567",
		`num(-1234)`:     "-1,234",
		`num(undefined)`: "0",
	} {
		if got := jsString(t, vm, expr); got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}

func TestFormatMoneyToleratesMissingFields(t *testing.T) {
	vm := newJSVM(t)
	tests := []struct{ expr, want string }{
		{`money({amount:"12500000",currency:"IDR"})`, "IDR 12,500,000"},
		{`money({currency:"IDR"})`, "IDR 0"},
		{`money(undefined)`, "—"},
		{`money({amount:"-3750000",currency:"IDR"})`, "IDR -3,750,000"},
	}
	for _, tc := range tests {
		if got := jsString(t, vm, tc.expr); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

func TestFormatPercentToleratesMissing(t *testing.T) {
	vm := newJSVM(t)
	for expr, want := range map[string]string{
		`pct(0.4)`:       "40%",
		`pct(undefined)`: "0%",
		`pct(0)`:         "0%",
	} {
		if got := jsString(t, vm, expr); got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}

func jsQuote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}
