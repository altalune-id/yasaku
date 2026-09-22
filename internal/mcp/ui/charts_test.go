package ui

import (
	"strings"
	"testing"
)

func TestDonutSVGGeometry(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `donutSVG([{share:0.5},{share:0.25},{share:0.25}])`)
	if !strings.HasPrefix(got, "<svg") {
		t.Fatalf("donutSVG did not return svg: %q", got)
	}
	if n := strings.Count(got, "<circle"); n != 4 {
		t.Errorf("donutSVG drew %d circles, want 4 (track + 3 slices)", n)
	}
	if strings.Contains(got, "NaN") {
		t.Errorf("donutSVG produced NaN:\n%s", got)
	}
}

func TestDonutSVGHandlesEmptyAndMissingShares(t *testing.T) {
	vm := newJSVM(t)
	if got := jsString(t, vm, `donutSVG([])`); got != "" {
		t.Errorf("donutSVG([]) = %q, want empty string", got)
	}
	got := jsString(t, vm, `donutSVG([{},{share:0.5}])`)
	if strings.Contains(got, "NaN") {
		t.Errorf("donutSVG leaked NaN on a missing share:\n%s", got)
	}
}

func TestCashflowSVGPlotsEveryPoint(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `cashflowSVG([
		{period:{name:"Jul"},income:{amount:"100"},expense:{amount:"60"}},
		{period:{name:"Aug"},income:{amount:"140"},expense:{amount:"90"}},
		{period:{name:"Sep"},income:{amount:"120"},expense:{amount:"130"}}
	])`)
	if n := strings.Count(got, "<rect"); n != 6 {
		t.Errorf("cashflowSVG drew %d bars, want 6 (income+expense per point)", n)
	}
	if strings.Contains(got, "NaN") {
		t.Errorf("cashflowSVG produced NaN:\n%s", got)
	}
}

func TestCashflowSVGSurvivesAllZero(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `cashflowSVG([{period:{name:"Jul"}},{period:{name:"Aug"}}])`)
	if strings.Contains(got, "NaN") || strings.Contains(got, "Infinity") {
		t.Errorf("all-zero cashflow must not divide by zero:\n%s", got)
	}
}
