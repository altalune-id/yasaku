package ui

import (
	"embed"
	"strconv"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
)

//go:embed testdata/*.json
var fixtures embed.FS

func renderFixture(t *testing.T, vm *goja.Runtime, tool, fixture string) (string, map[string]any) {
	t.Helper()
	raw, err := fixtures.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	v, err := vm.RunString("renderTool(" + strconv.Quote(tool) + ", " + string(raw) + ")")
	if err != nil {
		t.Fatalf("renderTool(%s): %v", tool, err)
	}
	out, ok := v.Export().(map[string]any)
	if !ok {
		t.Fatalf("renderTool returned %T, want an object", v.Export())
	}
	html, _ := out["html"].(string)
	actions, _ := out["actions"].(map[string]any)
	return html, actions
}

func TestFixturesMatchTheProtos(t *testing.T) {
	cases := []struct {
		file string
		msg  proto.Message
	}{
		{"period_report.json", &yasakuv1.PeriodReportResponse{}},
		{"period_report_sparse.json", &yasakuv1.PeriodReportResponse{}},
		{"cashflow_report.json", &yasakuv1.CashflowReportResponse{}},
		{"preview_close.json", &yasakuv1.PreviewCloseResponse{}},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := fixtures.ReadFile("testdata/" + tc.file)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, tc.msg); err != nil {
				t.Errorf("fixture no longer matches its proto: %v", err)
			}
		})
	}
}

func TestPeriodReportRendersTotals(t *testing.T) {
	vm := newJSVM(t)
	got, _ := renderFixture(t, vm, "period_report", "period_report.json")
	for _, want := range []string{"September 2026", "IDR 12,500,000", "IDR 8,750,000", "IDR 3,750,000", "Makan", "40%", "42"} {
		if !strings.Contains(got, want) {
			t.Errorf("period_report html missing %q\n%s", want, got)
		}
	}
}

func TestPeriodReportSurvivesSparsePayload(t *testing.T) {
	vm := newJSVM(t)
	got, _ := renderFixture(t, vm, "period_report", "period_report_sparse.json")
	if !strings.Contains(got, "October 2026") {
		t.Errorf("sparse period_report lost the period name:\n%s", got)
	}
	for _, bad := range []string{"undefined", "NaN", "[object Object]"} {
		if strings.Contains(got, bad) {
			t.Errorf("sparse period_report leaked %q — protojson omits zero values:\n%s", bad, got)
		}
	}
}

func TestPeriodReportEscapesCategoryNames(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`renderTool("period_report", {period:{name:"<img src=x onerror=alert(1)>"}}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(v.String(), "<img src=x") {
		t.Errorf("period name was not escaped:\n%s", v.String())
	}
}

func TestUnknownToolRendersNeutralPanel(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`renderTool("no_such_tool", {}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(v.String(), "ya-muted") {
		t.Errorf("unknown tool must render a neutral panel, got:\n%s", v.String())
	}
}

func TestCashflowReportRendersEveryPeriod(t *testing.T) {
	vm := newJSVM(t)
	got, actions := renderFixture(t, vm, "cashflow_report", "cashflow_report.json")
	for _, want := range []string{"Jul 2026", "Aug 2026", "Sep 2026", "<svg"} {
		if !strings.Contains(got, want) {
			t.Errorf("cashflow html missing %q", want)
		}
	}
	if len(actions) != 0 {
		t.Errorf("cashflow_report must declare no tool-calling actions, got %v", actions)
	}
}

func TestCashflowReportEmpty(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`renderTool("cashflow_report", {}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(v.String(), "undefined") || strings.Contains(v.String(), "NaN") {
		t.Errorf("empty cashflow leaked a placeholder:\n%s", v.String())
	}
}

func TestPreviewCloseOffersCloseAction(t *testing.T) {
	vm := newJSVM(t)
	got, actions := renderFixture(t, vm, "preview_close", "preview_close.json")
	if !strings.Contains(got, "IDR 3,750,000") {
		t.Errorf("preview_close html missing the net:\n%s", got)
	}
	a, ok := actions["close-period"]
	if !ok {
		t.Fatalf("preview_close must offer a close-period action, got %v", actions)
	}
	m := a.(map[string]any)
	if m["tool"] != "close_period" {
		t.Errorf("action tool = %v, want close_period", m["tool"])
	}
	args := m["args"].(map[string]any)
	if args["period"] != "per_2026_09" {
		t.Errorf("action args period = %v, want per_2026_09", args["period"])
	}
	if args["confirm"] != false {
		t.Errorf("action args confirm = %v, want false — compose must preview, never commit", args["confirm"])
	}
	if !strings.Contains(got, `data-action="close-period"`) {
		t.Errorf("the markup must carry data-action for boot.js delegation:\n%s", got)
	}
}
