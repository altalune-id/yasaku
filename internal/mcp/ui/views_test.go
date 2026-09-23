package ui

import (
	"embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
)

//go:embed testdata/*.json testdata/dom_stub.js
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
	html, ok := out["html"].(string)
	if !ok {
		t.Fatalf("renderTool(%s).html = %T, want string", tool, out["html"])
	}
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
		{"search_tx.json", &yasakuv1.SearchTransactionsResponse{}},
		{"wallets_list.json", &yasakuv1.ListWalletsResponse{}},
		{"wallets_list_sparse.json", &yasakuv1.ListWalletsResponse{}},
		{"wallet_detail.json", &yasakuv1.GetWalletResponse{}},
		{"wallet_totals.json", &yasakuv1.WalletTotalsResponse{}},
		{"wallet_detail_sparse.json", &yasakuv1.GetWalletResponse{}},
		{"wallet_totals_sparse.json", &yasakuv1.WalletTotalsResponse{}},
		{"category_list.json", &yasakuv1.ListCategoriesResponse{}},
		{"category_list_sparse.json", &yasakuv1.ListCategoriesResponse{}},
		{"period_list.json", &yasakuv1.ListPeriodsResponse{}},
		{"period_list_sparse.json", &yasakuv1.ListPeriodsResponse{}},
		{"project_list.json", &yasakuv1.ListProjectsResponse{}},
		{"project_list_sparse.json", &yasakuv1.ListProjectsResponse{}},
		{"current_period.json", &yasakuv1.GetCurrentPeriodResponse{}},
		{"current_period_sparse.json", &yasakuv1.GetCurrentPeriodResponse{}},
		{"now.json", &yasakuv1.NowResponse{}},
		{"now_sparse.json", &yasakuv1.NowResponse{}},
		{"create_wallet_needs.json", &yasakuv1.CreateWalletResponse{}},
		{"create_wallet_preview.json", &yasakuv1.CreateWalletResponse{}},
		{"create_wallet_result.json", &yasakuv1.CreateWalletResponse{}},
		{"update_wallet_preview.json", &yasakuv1.UpdateWalletResponse{}},
		{"archive_wallet_preview.json", &yasakuv1.ArchiveWalletResponse{}},
		{"adjust_balance_preview.json", &yasakuv1.AdjustBalanceResponse{}},
		{"adjust_balance_noop.json", &yasakuv1.AdjustBalanceResponse{}},
		{"record_expense_needs.json", &yasakuv1.RecordExpenseResponse{}},
		{"record_expense_preview.json", &yasakuv1.RecordExpenseResponse{}},
		{"record_income_preview.json", &yasakuv1.RecordIncomeResponse{}},
		{"record_transfer_preview.json", &yasakuv1.RecordTransferResponse{}},
		{"revise_tx_preview.json", &yasakuv1.ReviseTransactionResponse{}},
		{"delete_tx_result.json", &yasakuv1.DeleteTransactionResponse{}},
		{"close_period_preview.json", &yasakuv1.ClosePeriodResponse{}},
		{"close_period_result.json", &yasakuv1.ClosePeriodResponse{}},
		{"reopen_period_preview.json", &yasakuv1.ReopenPeriodResponse{}},
		{"create_category_preview.json", &yasakuv1.CreateCategoryResponse{}},
		{"mutation_org_needs.json", &yasakuv1.CreateWalletResponse{}},
		{"mutation_needs_no_candidates.json", &yasakuv1.CreateWalletResponse{}},
		{"record_batch_preview.json", &yasakuv1.RecordBatchResponse{}},
		{"record_batch_result.json", &yasakuv1.RecordBatchResponse{}},
		{"seed_preview.json", &yasakuv1.SeedDefaultCategoriesResponse{}},
		{"seed_result.json", &yasakuv1.SeedDefaultCategoriesResponse{}},
		{"seed_zero.json", &yasakuv1.SeedDefaultCategoriesResponse{}},
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
	if _, ok := actions["more"]; ok {
		t.Error("tx_list.json has no nextCursor, so no more action should be declared")
	}
}

func TestTxListOffersLoadMoreWhenPaged(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`JSON.stringify(renderTool("list_recent_tx", {transactions:[{id:"a",kind:"expense"}],nextCursor:"tok"}).actions)`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	const want = `{"more":{"tool":"list_recent_tx","args":{"cursor":"tok"}}}`
	if v.String() != want {
		t.Errorf("actions = %s, want %s", v.String(), want)
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
	for _, want := range []string{"IDR 99,000", "IDR 5,099,000", "IDR 120,000", "IDR 12,000", "IDR 108,000", "September 2026", "Tabungan"} {
		if !strings.Contains(got, want) {
			t.Errorf("wallet totals missing %q\n%s", want, got)
		}
	}
	for _, label := range []string{"Spendable", "Total", "Income", "Expense", "Net"} {
		if !strings.Contains(got, ">"+label+"<") {
			t.Errorf("wallet totals must label %q — the tool description promises income and expense:\n%s", label, got)
		}
	}
}

func TestWalletDetailAndTotalsEscapeNames(t *testing.T) {
	vm := newJSVM(t)
	const evil = `<img src=x onerror=alert(1)>`
	for _, expr := range []string{
		`renderTool("get_wallet", {wallet:{id:"w",name:"` + evil + `"}}).html`,
		`renderTool("wallet_totals", {wallets:[{wallet:{id:"w",name:"` + evil + `"}}]}).html`,
	} {
		v, err := vm.RunString(expr)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(v.String(), "<img src=x") {
			t.Errorf("name not escaped by %s:\n%s", expr, v.String())
		}
	}
}

func TestWalletViewsSurviveSparsePayloads(t *testing.T) {
	vm := newJSVM(t)
	for _, tc := range []struct{ tool, fixture string }{
		{"list_wallets", "wallets_list_sparse.json"},
		{"get_wallet", "wallet_detail_sparse.json"},
		{"wallet_totals", "wallet_totals_sparse.json"},
	} {
		got, _ := renderFixture(t, vm, tc.tool, tc.fixture)
		for _, bad := range []string{"undefined", "NaN", "[object Object]"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s on sparse payload leaked %q:\n%s", tc.tool, bad, got)
			}
		}
		if !strings.Contains(got, "Dompet Baru") {
			t.Errorf("%s dropped the one field the sparse payload does carry:\n%s", tc.tool, got)
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

func TestRegistryIsNotFooledByPrototypeKeys(t *testing.T) {
	vm := newJSVM(t)
	for _, name := range []string{"constructor", "toString", "__proto__", "hasOwnProperty"} {
		v, err := vm.RunString(`renderTool(` + strconv.Quote(name) + `, {}).html`)
		if err != nil {
			t.Fatalf("renderTool(%q): %v", name, err)
		}
		got := v.Export()
		s, ok := got.(string)
		if !ok {
			t.Errorf("renderTool(%q).html = %T, want string — boot.js assigns this straight to innerHTML", name, got)
			continue
		}
		if !strings.Contains(s, "ya-muted") {
			t.Errorf("renderTool(%q) must render the neutral panel, got:\n%s", name, s)
		}
	}
}

func TestTxSignIsNotFooledByPrototypeKeys(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`renderTool("list_recent_tx", {transactions:[{id:"x",kind:"constructor"}]}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(v.String(), "native code") {
		t.Errorf("a kind matching an Object.prototype member leaked a function into the glyph:\n%s", v.String())
	}
}

// TestEveryAnnotatedToolHasAView pins the two sides together: a tool annotated
// ui: "app" with no registered view renders "No view for …" in the host, and a
// registered view with no annotation is dead code. Both are silent today.
func TestEveryAnnotatedToolHasAView(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`Object.keys(VIEWS).sort().join(",")`)
	if err != nil {
		t.Fatalf("read VIEWS: %v", err)
	}
	registered := map[string]bool{}
	for _, n := range strings.Split(v.String(), ",") {
		registered[n] = true
	}

	annotated := map[string]bool{}
	root := filepath.Join("..", "..", "..", "gen", "go", "yasaku", "v1", "yasakuv1mcp")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read generated dir: %v", err)
	}
	nameRe := regexp.MustCompile(`Name:\s+"([a-z0-9_]+)"`)
	for _, e := range entries {
		src, err := os.ReadFile(filepath.Join(root, e.Name())) //nolint:gosec // generated tree
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, block := range strings.Split(string(src), "reg.Register(") {
			if !strings.Contains(block, "UIResourceURI:") {
				continue
			}
			if m := nameRe.FindStringSubmatch(block); m != nil {
				annotated[m[1]] = true
			}
		}
	}
	if len(annotated) == 0 {
		t.Fatal("found no annotated tools; the scrape is broken, not the code")
	}

	for name := range annotated {
		if !registered[name] {
			t.Errorf("tool %q carries ui: \"app\" but no view is registered — the host will render \"No view for\"", name)
		}
	}
	for name := range registered {
		if !annotated[name] {
			t.Errorf("view %q is registered but no tool carries ui: \"app\" for it — dead code", name)
		}
	}
}

func TestListViewsRenderContent(t *testing.T) {
	vm := newJSVM(t)
	tests := []struct {
		tool, fixture string
		want          []string
	}{
		{"list_categories", "category_list.json", []string{"Makan", "Transport", "Gaji"}},
		{"list_periods", "period_list.json", []string{"September 2026", "closed"}},
		{"list_projects", "project_list.json", []string{"Kas Harian", "Acme Group"}},
		{"current_period", "current_period.json", []string{"September 2026", "IDR 120,000"}},
		{"now", "now.json", []string{"Asia/Jakarta", "2026-09-22"}},
	}
	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			got, actions := renderFixture(t, vm, tc.tool, tc.fixture)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q\n%s", w, got)
				}
			}
			if len(actions) != 0 {
				t.Errorf("a read view declares no tool-calling actions, got %v", actions)
			}
		})
	}
}

func TestListViewsSurviveSparsePayloads(t *testing.T) {
	vm := newJSVM(t)
	tests := []struct{ tool, fixture, want string }{
		{"list_categories", "category_list_sparse.json", "Makan"},
		{"list_periods", "period_list_sparse.json", "October 2026"},
		{"list_projects", "project_list_sparse.json", "acme"},
		{"current_period", "current_period_sparse.json", "Sep 2026"},
		{"now", "now_sparse.json", "Asia/Jakarta"},
	}
	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			got, _ := renderFixture(t, vm, tc.tool, tc.fixture)
			for _, bad := range []string{"undefined", "NaN", "[object Object]"} {
				if strings.Contains(got, bad) {
					t.Errorf("leaked %q:\n%s", bad, got)
				}
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("dropped the one field the sparse payload carries:\n%s", got)
			}
		})
	}
}

func TestListViewsEscapeNames(t *testing.T) {
	vm := newJSVM(t)
	const evil = `<img src=x onerror=alert(1)>`
	for _, expr := range []string{
		`renderTool("list_categories", {categories:[{id:"c",name:"` + evil + `"}]}).html`,
		`renderTool("list_periods", {periods:[{id:"p",name:"` + evil + `"}]}).html`,
		`renderTool("list_projects", {projects:[{org:"o",orgName:"` + evil + `"}]}).html`,
		`renderTool("current_period", {period:{id:"p",name:"` + evil + `"}}).html`,
		`renderTool("now", {timezone:"` + evil + `",currentPeriod:{id:"p",name:"x"}}).html`,
	} {
		v, err := vm.RunString(expr)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(v.String(), "<img src=x") {
			t.Errorf("not escaped by %s:\n%s", expr, v.String())
		}
	}
}

//nolint:gochecknoglobals // the mutation tool list is a fixture for the safety test.
var mutationTools = []string{
	"create_wallet", "update_wallet", "archive_wallet", "adjust_balance",
	"record_expense", "record_income", "record_transfer", "revise_tx", "delete_tx",
	"close_period", "reopen_period", "create_category",
	"record_batch", "seed_default_categories",
}

//nolint:gochecknoglobals // the 12 single-entity mutations, whose preview is one object.
var entityMutationTools = mutationTools[:12]

// TestNoMutationEmitsConfirmOutsidePreview is the safety property: only a
// preview-phase commit control may ask the server to write.
func TestNoMutationEmitsConfirmOutsidePreview(t *testing.T) {
	vm := newJSVM(t)
	payloads := map[string]string{
		"needs":  `{needs:{needs:[{field:"wallet",reason:"which one?"}]}}`,
		"result": `{result:{id:"x"}}`,
		"empty":  `{warning:"nothing was written"}`,
	}
	for _, tool := range mutationTools {
		for phase, payload := range payloads {
			v, err := vm.RunString(`JSON.stringify(renderTool(` + strconv.Quote(tool) + `, ` + payload + `).actions)`)
			if err != nil {
				t.Fatalf("%s/%s: %v", tool, phase, err)
			}
			if strings.Contains(v.String(), `"confirm":true`) {
				t.Errorf("%s emits confirm:true in the %s phase: %s", tool, phase, v.String())
			}
		}
	}
}

func TestEveryMutationOffersCommitOnlyInPreview(t *testing.T) {
	vm := newJSVM(t)
	for _, tool := range entityMutationTools {
		v, err := vm.RunString(`JSON.stringify(renderTool(` + strconv.Quote(tool) + `, {preview:{id:"x",name:"n"}}).actions)`)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if !strings.Contains(v.String(), `"confirm":true`) {
			t.Errorf("%s offers no commit control in the preview phase: %s", tool, v.String())
		}
	}
}

func TestMutationNeedsRendersCandidatesAndSurvivesNone(t *testing.T) {
	vm := newJSVM(t)
	got, actions := renderFixture(t, vm, "create_wallet", "create_wallet_needs.json")
	for _, want := range []string{"<select", `name="kind"`, "cash", "savings", "kind is required"} {
		if !strings.Contains(got, want) {
			t.Errorf("needs phase missing %q\n%s", want, got)
		}
	}
	if _, ok := actions["edit"]; !ok {
		t.Errorf("needs phase must offer an edit action, got %v", actions)
	}

	bare, _ := renderFixture(t, vm, "create_wallet", "mutation_needs_no_candidates.json")
	if strings.Contains(bare, "<select") {
		t.Errorf("a need with no candidates has nothing to select from:\n%s", bare)
	}
	if !strings.Contains(bare, "you belong to no org") {
		t.Errorf("the reason must still be shown:\n%s", bare)
	}
}

func TestMutationMapsTargetNeedsToTargetFields(t *testing.T) {
	vm := newJSVM(t)
	got, _ := renderFixture(t, vm, "create_wallet", "mutation_org_needs.json")
	if !strings.Contains(got, `name="target.org"`) {
		t.Errorf(`an "org" need must submit as target.org, not org — the server ignores a bare org:\n%s`, got)
	}
}

func TestClosePeriodRendersBothAsymmetricPhases(t *testing.T) {
	vm := newJSVM(t)
	prev, _ := renderFixture(t, vm, "close_period", "close_period_preview.json")
	if !strings.Contains(prev, "IDR 108,000") {
		t.Errorf("close_period preview is a Snapshot and must render its totals:\n%s", prev)
	}
	res, _ := renderFixture(t, vm, "close_period", "close_period_result.json")
	if !strings.Contains(res, "September 2026") {
		t.Errorf("close_period result is a Period:\n%s", res)
	}
}

func TestAdjustBalanceNoOpIsASuccessNotAForm(t *testing.T) {
	vm := newJSVM(t)
	got, actions := renderFixture(t, vm, "adjust_balance", "adjust_balance_noop.json")
	if !strings.Contains(got, "already held that balance") {
		t.Errorf("the empty phase must show the server's warning:\n%s", got)
	}
	if len(actions) != 0 {
		t.Errorf("a completed no-op offers nothing to submit, got %v", actions)
	}
}

func TestNoFixtureCarriesBothPreviewAndResult(t *testing.T) {
	entries, err := fixtures.ReadDir("testdata")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := fixtures.ReadFile("testdata/" + e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		_, hasPreview := m["preview"]
		_, hasResult := m["result"]
		if hasPreview && hasResult {
			t.Errorf("%s carries both preview and result; no handler emits both", e.Name())
		}
	}
}

func TestBatchNumbersRowZeroAndMarksBothFailureChannels(t *testing.T) {
	vm := newJSVM(t)
	got, actions := renderFixture(t, vm, "record_batch", "record_batch_preview.json")
	if strings.Contains(got, "undefined") {
		t.Errorf("BatchOutcome.index is omitted for row 0; the view must not print it raw:\n%s", got)
	}
	if !strings.Contains(got, "Row 1") {
		t.Errorf("the first row must be numbered from the omitted index:\n%s", got)
	}
	if !strings.Contains(got, "YSK404") || !strings.Contains(got, "not found") {
		t.Errorf("a refused row must show its errorCode and message:\n%s", got)
	}
	if _, ok := actions["commit"]; !ok {
		t.Errorf("a batch preview must offer a commit, got %v", actions)
	}

	res, _ := renderFixture(t, vm, "record_batch", "record_batch_result.json")
	if !strings.Contains(res, "period is closed") {
		t.Errorf("a row carrying error with no errorCode must still be marked failed:\n%s", res)
	}
}

func TestSeedZeroOffersNoCommit(t *testing.T) {
	vm := newJSVM(t)
	got, actions := renderFixture(t, vm, "seed_default_categories", "seed_zero.json")
	if len(actions) != 0 {
		t.Errorf("nothing to seed means nothing to commit, got %v", actions)
	}
	if strings.Contains(got, "undefined") || strings.Contains(got, "NaN") {
		t.Errorf("seed zero leaked a placeholder:\n%s", got)
	}

	prev, prevActions := renderFixture(t, vm, "seed_default_categories", "seed_preview.json")
	if !strings.Contains(prev, "12") {
		t.Errorf("seed preview must show the count:\n%s", prev)
	}
	if _, ok := prevActions["commit"]; !ok {
		t.Errorf("seed preview must offer a commit, got %v", prevActions)
	}
}

func TestBulkViewsTolerateAScalarPreview(t *testing.T) {
	vm := newJSVM(t)
	for _, tool := range []string{"record_batch", "seed_default_categories"} {
		v, err := vm.RunString(`renderTool(` + strconv.Quote(tool) + `, {preview:{id:"x"}}).html`)
		if err != nil {
			t.Fatalf("%s threw on a non-array preview: %v", tool, err)
		}
		if strings.Contains(v.String(), "undefined") {
			t.Errorf("%s leaked a placeholder:\n%s", tool, v.String())
		}
	}
}

// TestTxDatePrefersTheCivilDate pins txDate on the civil date, falling back to the UTC instant only when no date is present.
func TestTxDatePrefersTheCivilDate(t *testing.T) {
	vm := newJSVM(t)
	v, err := vm.RunString(`renderTool("list_recent_tx", {transactions:[
		{id:"a",kind:"expense",date:"2026-09-23",occurredAt:"2026-09-22T19:30:00Z"}
	]}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(v.String(), "2026-09-23") {
		t.Errorf("the civil date must win over the UTC instant:\n%s", v.String())
	}
	if strings.Contains(v.String(), "2026-09-22") {
		t.Errorf("the UTC day must not be shown when a civil date is present:\n%s", v.String())
	}

	fallback, err := vm.RunString(`renderTool("list_recent_tx", {transactions:[
		{id:"a",kind:"expense",occurredAt:"2026-09-22T19:30:00Z"}
	]}).html`)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(fallback.String(), "2026-09-22") {
		t.Errorf("with no civil date the instant is all we have:\n%s", fallback.String())
	}
}

func TestPreviewsAndBatchRowsRenderTheCivilDate(t *testing.T) {
	vm := newJSVM(t)
	tests := []struct {
		tool, fixture, civil, utc string
	}{
		{"list_recent_tx", "tx_list.json", "2026-09-15", "2026-09-14"},
		{"get_wallet", "wallet_detail.json", "2026-09-01", "2026-08-31"},
		{"record_expense", "record_expense_preview.json", "2026-09-22", "2026-09-21"},
		{"record_income", "record_income_preview.json", "2026-09-01", "2026-08-31"},
		{"record_transfer", "record_transfer_preview.json", "2026-09-10", "2026-09-09"},
		{"adjust_balance", "adjust_balance_preview.json", "2026-09-22", "2026-09-21"},
		{"record_batch", "record_batch_preview.json", "2026-09-22", "2026-09-21"},
		{"record_batch", "record_batch_result.json", "2026-09-22", "2026-09-21"},
		{"search_tx", "search_tx.json", "2026-09-15", ""},
		{"revise_tx", "revise_tx_preview.json", "2026-09-15", ""},
		{"delete_tx", "delete_tx_result.json", "2026-09-15", ""},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			got, _ := renderFixture(t, vm, tc.tool, tc.fixture)
			if !strings.Contains(got, tc.civil) {
				t.Errorf("%s must render the project-timezone day %s:\n%s", tc.fixture, tc.civil, got)
			}
			if tc.utc != "" && strings.Contains(got, tc.utc) {
				t.Errorf("%s leaked the UTC day %s from occurredAt:\n%s", tc.fixture, tc.utc, got)
			}
		})
	}
}
