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
		{"tx_list.json", &yasakuv1.ListTransactionsResponse{}},
		{"tx_list_sparse.json", &yasakuv1.ListTransactionsResponse{}},
		{"wallets_list.json", &yasakuv1.ListWalletsResponse{}},
		{"wallets_list_sparse.json", &yasakuv1.ListWalletsResponse{}},
		{"wallet_detail.json", &yasakuv1.GetWalletResponse{}},
		{"wallet_totals.json", &yasakuv1.WalletTotalsResponse{}},
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

func TestTxListRendersEveryRow(t *testing.T) {
	vm := newJSVM(t)
	got, actions := renderFixture(t, vm, "list_recent_tx", "tx_list.json")
	for _, want := range []string{"Nasi goreng", "IDR 12,000", "IDR 120,000", "Food &amp; Drinks", "Tabungan", "Sisihkan"} {
		if !strings.Contains(got, want) {
			t.Errorf("tx list missing %q\n%s", want, got)
		}
	}
	if !strings.Contains(got, ">In<") || !strings.Contains(got, ">Out<") {
		t.Error("tx list must show totalIn and totalOut")
	}
	if strings.Contains(got, "Food & Drinks") {
		t.Error("an ampersand in a category name must be escaped")
	}
	if len(actions) != 0 {
		t.Errorf("tx list declares no tool-calling actions, got %v", actions)
	}
}

func TestSearchTxUsesTheSameRenderer(t *testing.T) {
	vm := newJSVM(t)
	a, _ := renderFixture(t, vm, "list_recent_tx", "tx_list.json")
	b, _ := renderFixture(t, vm, "search_tx", "tx_list.json")
	if a != b {
		t.Error("search_tx and list_recent_tx return the same shape and must render identically")
	}
}

func TestTxListSurvivesSparsePayload(t *testing.T) {
	vm := newJSVM(t)
	got, _ := renderFixture(t, vm, "list_recent_tx", "tx_list_sparse.json")
	for _, bad := range []string{"undefined", "NaN", "[object Object]"} {
		if strings.Contains(got, bad) {
			t.Errorf("sparse tx list leaked %q:\n%s", bad, got)
		}
	}
}

func TestTxListEscapesNotes(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`renderTool("list_recent_tx", {transactions:[{id:"x",kind:"expense",note:"<script>alert(1)</script>"}]}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(v.String(), "<script>alert") {
		t.Errorf("note was not escaped:\n%s", v.String())
	}
}

func TestWalletListRendersBalancesAndKinds(t *testing.T) {
	vm := newJSVM(t)
	got, _ := renderFixture(t, vm, "list_wallets", "wallets_list.json")
	for _, want := range []string{"Makan Daily Cash", "IDR 108,000", "Tabungan", "IDR 5,000,000", "cash", "savings"} {
		if !strings.Contains(got, want) {
			t.Errorf("wallet list missing %q\n%s", want, got)
		}
	}
	if !strings.Contains(got, "excluded") {
		t.Errorf("a wallet with excludeFromTotal must be marked:\n%s", got)
	}
}

func TestWalletDetailShowsRecentTransactions(t *testing.T) {
	vm := newJSVM(t)
	got, _ := renderFixture(t, vm, "get_wallet", "wallet_detail.json")
	for _, want := range []string{"Makan Daily Cash", "IDR 108,000", "Nasi goreng", "IDR 120,000"} {
		if !strings.Contains(got, want) {
			t.Errorf("wallet detail missing %q\n%s", want, got)
		}
	}
}

func TestWalletTotalsDistinguishesSpendableFromTotal(t *testing.T) {
	vm := newJSVM(t)
	got, _ := renderFixture(t, vm, "wallet_totals", "wallet_totals.json")
	for _, want := range []string{"IDR 108,000", "IDR 5,108,000", "September 2026", "Tabungan"} {
		if !strings.Contains(got, want) {
			t.Errorf("wallet totals missing %q\n%s", want, got)
		}
	}
	if !strings.Contains(got, "Spendable") {
		t.Errorf("spendable total must be labelled distinctly from total:\n%s", got)
	}
}

func TestWalletViewsSurviveSparsePayloads(t *testing.T) {
	vm := newJSVM(t)
	for _, tc := range []struct{ tool, fixture string }{
		{"list_wallets", "wallets_list_sparse.json"},
		{"get_wallet", "tx_list_sparse.json"},
		{"wallet_totals", "tx_list_sparse.json"},
	} {
		got, _ := renderFixture(t, vm, tc.tool, tc.fixture)
		for _, bad := range []string{"undefined", "NaN", "[object Object]"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s on sparse payload leaked %q:\n%s", tc.tool, bad, got)
			}
		}
	}
}

func TestWalletNamesAreEscaped(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`renderTool("list_wallets", {wallets:[{id:"w",name:"<img src=x onerror=alert(1)>"}]}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(v.String(), "<img src=x") {
		t.Errorf("wallet name was not escaped:\n%s", v.String())
	}
}
