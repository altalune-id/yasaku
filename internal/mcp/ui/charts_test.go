package ui

import (
	"strings"
	"testing"
)

func TestDonutModelGeometry(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `JSON.stringify(donutModel([{share:0.5},{share:0.25},{share:0.25}]))`)
	if n := strings.Count(got, `"stroke"`); n != 3 {
		t.Errorf("donutModel drew %d slices, want 3:\n%s", n, got)
	}
	if !strings.Contains(got, `"offset":"-131.95"`) || !strings.Contains(got, `"dash":"131.95 131.95"`) {
		t.Errorf("donutModel geometry drifted:\n%s", got)
	}
	if strings.Contains(got, "NaN") {
		t.Errorf("donutModel produced NaN:\n%s", got)
	}
}

func TestDonutModelHandlesEmptyAndMissingShares(t *testing.T) {
	vm := newJSVM(t)
	if got := jsString(t, vm, `String(donutModel([]).empty)`); got != "true" {
		t.Errorf("donutModel([]).empty = %s, want true", got)
	}
	if got := jsString(t, vm, `String(donutModel(undefined).empty)`); got != "true" {
		t.Errorf("donutModel(undefined).empty = %s, want true", got)
	}
	if got := jsString(t, vm, `JSON.stringify(donutModel([{},{share:0.5}]))`); strings.Contains(got, "NaN") {
		t.Errorf("donutModel leaked NaN on a missing share:\n%s", got)
	}
}

func TestCashflowModelPlotsEveryPoint(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `JSON.stringify(cashflowModel([
		{period:{name:"Jul"},income:{amount:"100"},expense:{amount:"60"}},
		{period:{name:"Aug"},income:{amount:"140"},expense:{amount:"90"}},
		{period:{name:"Sep"},income:{amount:"120"},expense:{amount:"130"}}
	]))`)
	if n := strings.Count(got, `"fill"`); n != 6 {
		t.Errorf("cashflowModel drew %d bars, want 6 (income+expense per point)", n)
	}
	if n := strings.Count(got, `"text"`); n != 3 {
		t.Errorf("cashflowModel drew %d labels, want 3", n)
	}
	if strings.Contains(got, "NaN") {
		t.Errorf("cashflowModel produced NaN:\n%s", got)
	}
}

func TestCashflowModelSurvivesAllZero(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `JSON.stringify(cashflowModel([{period:{name:"Jul"}},{period:{name:"Aug"}}]))`)
	if strings.Contains(got, "NaN") || strings.Contains(got, "Infinity") {
		t.Errorf("all-zero cashflow must not divide by zero:\n%s", got)
	}
	if got := jsString(t, vm, `JSON.stringify(cashflowModel([]))`); strings.Contains(got, "NaN") || strings.Contains(got, "Infinity") {
		t.Errorf("an empty cashflow must not divide by zero:\n%s", got)
	}
}

// TestChartAttributesCannotBreakOut: a slice colour lands in an SVG stroke attribute, so only an allow-listed token may reach it.
func TestChartAttributesCannotBreakOut(t *testing.T) {
	vm := newJSVM(t)
	got := jsString(t, vm, `donutModel([{share:1,category:{color:'#000" onmouseover="alert(1)'}}]).segments[0].stroke`)
	if strings.Contains(got, "onmouseover") || strings.ContainsAny(got, `"<>`) {
		t.Errorf("a slice colour carried attribute breakout into the stroke: %q", got)
	}
	label := jsString(t, vm, `cashflowModel([{period:{name:{__raw:"<script>"}}}]).labels[0].text`)
	if label != "" {
		t.Errorf("a forged label object reached the chart: %q", label)
	}
}
