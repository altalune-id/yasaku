package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
)

// NOTE: the 27 yasaku tool names are a wire contract with MCP hosts; a rename here strands every host that cached the old name.
//
//nolint:gochecknoglobals // a wire-contract table has to be package level.
var yasakuViewTools = []string{
	"list_wallets", "get_wallet", "wallet_totals",
	"list_recent_tx", "search_tx",
	"period_report", "cashflow_report", "preview_close",
	"list_categories", "list_periods", "list_projects", "current_period", "now",
	"create_wallet", "update_wallet", "archive_wallet", "adjust_balance",
	"record_expense", "record_income", "record_transfer", "revise_tx", "delete_tx",
	"reopen_period", "create_category", "close_period",
	"record_batch", "seed_default_categories",
}

//nolint:gochecknoglobals // the mutation tool list is a fixture for the safety tests.
var mutationTools = []string{
	"create_wallet", "update_wallet", "archive_wallet", "adjust_balance",
	"record_expense", "record_income", "record_transfer", "revise_tx", "delete_tx",
	"close_period", "reopen_period", "create_category",
	"record_batch", "seed_default_categories",
}

//nolint:gochecknoglobals // the 12 single-entity mutations, whose preview is one object.
var entityMutationTools = mutationTools[:12]

type viewFixture struct{ tool, fixture string }

//nolint:gochecknoglobals // a fixture table has to be package level.
var viewFixtures = []viewFixture{
	{"period_report", "period_report.json"},
	{"period_report", "period_report_sparse.json"},
	{"cashflow_report", "cashflow_report.json"},
	{"preview_close", "preview_close.json"},
	{"list_recent_tx", "tx_list.json"},
	{"list_recent_tx", "tx_list_sparse.json"},
	{"search_tx", "search_tx.json"},
	{"list_wallets", "wallets_list.json"},
	{"list_wallets", "wallets_list_sparse.json"},
	{"get_wallet", "wallet_detail.json"},
	{"get_wallet", "wallet_detail_sparse.json"},
	{"wallet_totals", "wallet_totals.json"},
	{"wallet_totals", "wallet_totals_sparse.json"},
	{"list_categories", "category_list.json"},
	{"list_categories", "category_list_sparse.json"},
	{"list_periods", "period_list.json"},
	{"list_periods", "period_list_sparse.json"},
	{"list_projects", "project_list.json"},
	{"list_projects", "project_list_sparse.json"},
	{"current_period", "current_period.json"},
	{"current_period", "current_period_sparse.json"},
	{"now", "now.json"},
	{"now", "now_sparse.json"},
	{"create_wallet", "create_wallet_needs.json"},
	{"create_wallet", "create_wallet_preview.json"},
	{"create_wallet", "create_wallet_result.json"},
	{"create_wallet", "mutation_org_needs.json"},
	{"create_wallet", "mutation_needs_no_candidates.json"},
	{"update_wallet", "update_wallet_preview.json"},
	{"archive_wallet", "archive_wallet_preview.json"},
	{"adjust_balance", "adjust_balance_preview.json"},
	{"adjust_balance", "adjust_balance_noop.json"},
	{"record_expense", "record_expense_needs.json"},
	{"record_expense", "record_expense_preview.json"},
	{"record_income", "record_income_preview.json"},
	{"record_transfer", "record_transfer_preview.json"},
	{"revise_tx", "revise_tx_preview.json"},
	{"delete_tx", "delete_tx_result.json"},
	{"close_period", "close_period_preview.json"},
	{"close_period", "close_period_result.json"},
	{"reopen_period", "reopen_period_preview.json"},
	{"create_category", "create_category_preview.json"},
	{"record_batch", "record_batch_preview.json"},
	{"record_batch", "record_batch_result.json"},
	{"seed_default_categories", "seed_preview.json"},
	{"seed_default_categories", "seed_result.json"},
	{"seed_default_categories", "seed_zero.json"},
}

// NOTE: goja has no DOM, so Lit is stubbed to inert values; the real registerView calls run and the render layer is covered by views_browser_test.go.
const litStub = `
globalThis.__lit = {
	LitElement: class {},
	html: function () { return null; },
	css: function () { return ""; },
	nothing: null,
};
globalThis.customElements = { define: function () {} };
`

func newViewsVM(t *testing.T) *goja.Runtime {
	t.Helper()
	vm := goja.New()
	if _, err := vm.RunString(litStub); err != nil {
		t.Fatalf("eval lit stub: %v", err)
	}
	for _, p := range scriptParts {
		if domParts[p] {
			continue
		}
		if _, err := vm.RunString(mustRead(t, p)); err != nil {
			t.Fatalf("eval %s: %v", p, err)
		}
	}
	return vm
}

func modelInline(t *testing.T, vm *goja.Runtime, tool, payload string) (map[string]any, map[string]any) {
	t.Helper()
	v, err := vm.RunString("renderTool(" + strconv.Quote(tool) + ", " + payload + ")")
	if err != nil {
		t.Fatalf("renderTool(%s): %v", tool, err)
	}
	out, ok := v.Export().(map[string]any)
	if !ok {
		t.Fatalf("renderTool returned %T, want an object", v.Export())
	}
	model, ok := out["model"].(map[string]any)
	if !ok {
		t.Fatalf("renderTool(%s).model = %T, want an object", tool, out["model"])
	}
	actions, _ := out["actions"].(map[string]any)
	return model, actions
}

func modelJSON(t *testing.T, vm *goja.Runtime, tool, payload string) string {
	t.Helper()
	return jsString(t, vm, "JSON.stringify(renderTool("+strconv.Quote(tool)+", "+payload+").model)")
}

func actionsJSON(t *testing.T, vm *goja.Runtime, tool, payload string) string {
	t.Helper()
	return jsString(t, vm, "JSON.stringify(renderTool("+strconv.Quote(tool)+", "+payload+").actions)")
}

func requireAll(t *testing.T, what, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s is missing %q:\n%s", what, w, got)
		}
	}
}

func requireNoPlaceholder(t *testing.T, what, got string) {
	t.Helper()
	for _, bad := range []string{"undefined", "NaN", "[object Object]", "Infinity"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s leaked %q — protojson omits zero values:\n%s", what, bad, got)
		}
	}
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
			if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal([]byte(fixtureJSON(t, tc.file)), tc.msg); err != nil {
				t.Errorf("fixture no longer matches its proto: %v", err)
			}
		})
	}
}

func TestEveryYasakuToolHasExactlyOneView(t *testing.T) {
	vm := newViewsVM(t)
	registered := strings.Split(jsString(t, vm, `Object.keys(VIEWS).sort().join(",")`), ",")
	want := append(slices.Clone(yasakuViewTools), "blog_list")
	sort.Strings(want)
	if !slices.Equal(registered, want) {
		t.Errorf("registered views = %v\nwant %v", registered, want)
	}
	if len(yasakuViewTools) != 27 {
		t.Errorf("yasaku declares %d view tools, want 27", len(yasakuViewTools))
	}

	var bundle strings.Builder
	for _, p := range scriptParts {
		bundle.WriteString(mustRead(t, p))
	}
	for _, name := range yasakuViewTools {
		if n := strings.Count(bundle.String(), `registerView("`+name+`",`); n != 1 {
			t.Errorf("%q is registered %d times, want exactly 1", name, n)
		}
		if got := jsString(t, vm, `typeof VIEWS[`+jsQuote(name)+`].model + "," + typeof VIEWS[`+jsQuote(name)+`].template`); got != "function,function" {
			t.Errorf("%q registered model,template = %s, want function,function", name, got)
		}
	}
}

// TestEveryAnnotatedToolHasAView pins both sides: an annotated tool with no view renders "No view for" in the host, and a view with no annotated tool is dead code.
func TestEveryAnnotatedToolHasAView(t *testing.T) {
	vm := newViewsVM(t)
	registered := map[string]bool{}
	for _, n := range strings.Split(jsString(t, vm, `Object.keys(VIEWS).join(",")`), ",") {
		registered[n] = true
	}

	dirs, err := filepath.Glob(filepath.Join("..", "..", "..", "gen", "go", "*", "v1", "*mcp"))
	if err != nil {
		t.Fatalf("glob generated dirs: %v", err)
	}
	nameConst := regexp.MustCompile(`const (\w+) = "([a-z0-9_]+)"`)
	nameRe := regexp.MustCompile(`Name:\s+(?:"([a-z0-9_]+)"|(\w+ToolName))`)
	annotated := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			src, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // generated tree
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			consts := map[string]string{}
			for _, m := range nameConst.FindAllStringSubmatch(string(src), -1) {
				consts[m[1]] = m[2]
			}
			for _, block := range strings.Split(string(src), "reg.Register(")[1:] {
				if !strings.Contains(block, "UIResourceURI:") && !strings.Contains(block, "UI:") {
					continue
				}
				m := nameRe.FindStringSubmatch(block)
				if m == nil {
					continue
				}
				name := m[1]
				if name == "" {
					name = consts[m[2]]
				}
				annotated[name] = true
			}
		}
	}
	if len(annotated) == 0 {
		t.Fatal("found no annotated tools; the scrape is broken, not the code")
	}
	for name := range annotated {
		if !registered[name] {
			t.Errorf("tool %q carries a ui annotation but no view is registered — the host will render \"No view for\"", name)
		}
	}
	for name := range registered {
		if !annotated[name] {
			t.Errorf("view %q is registered but no generated tool carries a ui annotation for it — dead code", name)
		}
	}
}

func walkLeaves(v any, path string, visit func(path string, leaf any)) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			walkLeaves(child, path+"."+k, visit)
		}
	case []any:
		for i, child := range x {
			walkLeaves(child, path+"["+strconv.Itoa(i)+"]", visit)
		}
	default:
		visit(path, v)
	}
}

// TestEveryViewModelCarriesOnlyStringsAndFlags: anything else reaching the render layer would carry the tool result's own shape past the model.
func TestEveryViewModelCarriesOnlyStringsAndFlags(t *testing.T) {
	vm := newViewsVM(t)
	for _, f := range viewFixtures {
		t.Run(f.fixture, func(t *testing.T) {
			model, _ := modelFixture(t, vm, f.tool, f.fixture)
			walkLeaves(model, "model", func(path string, leaf any) {
				switch s := leaf.(type) {
				case string:
					requireNoPlaceholder(t, path, s)
				case bool:
				default:
					t.Errorf("%s = %#v (%T); a view model leaf must be a string or a flag", path, leaf, leaf)
				}
			})
		})
	}
}

func TestPeriodReportModelCarriesTotals(t *testing.T) {
	vm := newViewsVM(t)
	got := modelJSON(t, vm, "period_report", fixtureJSON(t, "period_report.json"))
	requireAll(t, "period_report model", got, "September 2026", "IDR 12,500,000", "IDR 8,750,000", "IDR 3,750,000", "Makan", "40%", `"value":"42"`)
	model, actions := modelFixture(t, vm, "period_report", "period_report.json")
	if model["refresh"] != "refresh" {
		t.Errorf("refresh = %v, want the declared action id", model["refresh"])
	}
	a, ok := actions["refresh"].(map[string]any)
	if !ok || a["tool"] != "period_report" || a["args"].(map[string]any)["period"] != "per_2026_09" {
		t.Errorf("refresh action = %v", actions["refresh"])
	}
}

func TestPeriodReportSurvivesSparsePayload(t *testing.T) {
	vm := newViewsVM(t)
	got := modelJSON(t, vm, "period_report", fixtureJSON(t, "period_report_sparse.json"))
	requireAll(t, "sparse period_report", got, "October 2026")
	requireNoPlaceholder(t, "sparse period_report", got)
}

func TestUnknownToolIsMissingNotAPanel(t *testing.T) {
	vm := newViewsVM(t)
	if got := jsString(t, vm, `String(renderTool("no_such_tool", {}).missing)`); got != "true" {
		t.Errorf("an unregistered tool must report missing, got %s", got)
	}
}

func TestCashflowReportModelPlotsEveryPeriod(t *testing.T) {
	vm := newViewsVM(t)
	got := modelJSON(t, vm, "cashflow_report", fixtureJSON(t, "cashflow_report.json"))
	requireAll(t, "cashflow model", got, "Jul 2026", "Aug 2026", "Sep 2026", "Cashflow, last 3 periods")
	model, actions := modelFixture(t, vm, "cashflow_report", "cashflow_report.json")
	if n := len(model["chart"].(map[string]any)["bars"].([]any)); n != 6 {
		t.Errorf("chart bars = %d, want 6 (income+expense per point)", n)
	}
	if len(actions) != 0 {
		t.Errorf("cashflow_report must declare no tool-calling actions, got %v", actions)
	}
}

func TestCashflowReportEmpty(t *testing.T) {
	vm := newViewsVM(t)
	model, _ := modelInline(t, vm, "cashflow_report", `{}`)
	if model["empty"] != true {
		t.Errorf("empty = %v, want true", model["empty"])
	}
	requireNoPlaceholder(t, "empty cashflow", modelJSON(t, vm, "cashflow_report", `{}`))
}

func TestPreviewCloseOffersCloseAction(t *testing.T) {
	vm := newViewsVM(t)
	got := modelJSON(t, vm, "preview_close", fixtureJSON(t, "preview_close.json"))
	requireAll(t, "preview_close model", got, "IDR 3,750,000")
	model, actions := modelFixture(t, vm, "preview_close", "preview_close.json")
	if model["close"] != "close-period" {
		t.Errorf("model.close = %v, want close-period", model["close"])
	}
	a, ok := actions["close-period"].(map[string]any)
	if !ok {
		t.Fatalf("preview_close must offer a close-period action, got %v", actions)
	}
	if a["tool"] != "close_period" {
		t.Errorf("action tool = %v, want close_period", a["tool"])
	}
	args := a["args"].(map[string]any)
	if args["period"] != "per_2026_09" {
		t.Errorf("action args period = %v, want per_2026_09", args["period"])
	}
	if args["confirm"] != false {
		t.Errorf("action args confirm = %v, want false — compose must preview, never commit", args["confirm"])
	}
}

func TestTxListModelCarriesEveryRow(t *testing.T) {
	vm := newViewsVM(t)
	got := modelJSON(t, vm, "list_recent_tx", fixtureJSON(t, "tx_list.json"))
	requireAll(t, "tx list model", got, "Nasi goreng", "IDR 12,000", "IDR 120,000", "Food & Drinks", "Tabungan", "Sisihkan", `"label":"In"`, `"label":"Out"`)
	model, actions := modelFixture(t, vm, "list_recent_tx", "tx_list.json")
	if model["totals"] != true {
		t.Error("tx list must show totalIn and totalOut")
	}
	if _, ok := actions["more"]; ok {
		t.Error("tx_list.json has no nextCursor, so no more action should be declared")
	}
}

func TestTxListOffersLoadMoreWhenPaged(t *testing.T) {
	vm := newViewsVM(t)
	got := actionsJSON(t, vm, "list_recent_tx", `{transactions:[{id:"a",kind:"expense"}],nextCursor:"tok"}`)
	const want = `{"more":{"tool":"list_recent_tx","args":{"cursor":"tok"}}}`
	if got != want {
		t.Errorf("actions = %s, want %s", got, want)
	}
}

func TestSearchTxModelsLikeTheRecentList(t *testing.T) {
	vm := newViewsVM(t)
	a := modelJSON(t, vm, "list_recent_tx", fixtureJSON(t, "tx_list.json"))
	b := modelJSON(t, vm, "search_tx", fixtureJSON(t, "tx_list.json"))
	if a != b {
		t.Error("search_tx and list_recent_tx return the same shape and must model identically")
	}
}

// TestSearchTxLoadMorePagesTheSearch: a cursor from search_tx is meaningless to list_recent_tx, and the wrong tool also drops the query.
func TestSearchTxLoadMorePagesTheSearch(t *testing.T) {
	vm := newViewsVM(t)
	got := actionsJSON(t, vm, "search_tx", fixtureJSON(t, "search_tx.json"))
	const want = `{"more":{"tool":"search_tx","args":{"cursor":"tok"}}}`
	if got != want {
		t.Errorf("actions = %s, want %s", got, want)
	}
}

func TestTxListSurvivesSparsePayload(t *testing.T) {
	vm := newViewsVM(t)
	requireNoPlaceholder(t, "sparse tx list", modelJSON(t, vm, "list_recent_tx", fixtureJSON(t, "tx_list_sparse.json")))
}

func TestWalletListModelCarriesBalancesAndKinds(t *testing.T) {
	vm := newViewsVM(t)
	got := modelJSON(t, vm, "list_wallets", fixtureJSON(t, "wallets_list.json"))
	requireAll(t, "wallet list model", got, "Makan Daily Cash", "IDR 108,000", "Tabungan", "IDR 5,000,000", "cash", "savings · BCA")
	model, _ := modelFixture(t, vm, "list_wallets", "wallets_list.json")
	if row := model["rows"].([]any)[1].(map[string]any); row["excluded"] != true {
		t.Errorf("a wallet with excludeFromTotal must be marked: %v", row)
	}
}

func TestWalletListModelMarksExcludedAndArchived(t *testing.T) {
	vm := newViewsVM(t)
	model, _ := modelInline(t, vm, "list_wallets", `{"wallets":[{"name":"Cash","excludeFromTotal":true,"archived":true,"balance":{"amount":"1000","currency":"IDR"}}]}`)
	row := model["rows"].([]any)[0].(map[string]any)
	if row["excluded"] != true || row["archived"] != true {
		t.Fatalf("row = %v", row)
	}
	if empty, _ := modelInline(t, vm, "list_wallets", `{}`); empty["empty"] != true {
		t.Errorf("no wallets must model as empty, got %v", empty)
	}
}

func TestWalletDetailModelCarriesRecentTransactions(t *testing.T) {
	vm := newViewsVM(t)
	got := modelJSON(t, vm, "get_wallet", fixtureJSON(t, "wallet_detail.json"))
	requireAll(t, "wallet detail model", got, "Makan Daily Cash", "IDR 108,000", "Nasi goreng", "IDR 120,000")
}

func TestWalletTotalsDistinguishesSpendableFromTotal(t *testing.T) {
	vm := newViewsVM(t)
	got := modelJSON(t, vm, "wallet_totals", fixtureJSON(t, "wallet_totals.json"))
	requireAll(t, "wallet totals model", got, "IDR 99,000", "IDR 5,099,000", "IDR 120,000", "IDR 12,000", "IDR 108,000", "September 2026", "Tabungan")
	for _, label := range []string{"Spendable", "Total", "Income", "Expense", "Net"} {
		if !strings.Contains(got, `"label":"`+label+`"`) {
			t.Errorf("wallet totals must label %q — the tool description promises income and expense:\n%s", label, got)
		}
	}
}

func TestWalletViewsSurviveSparsePayloads(t *testing.T) {
	vm := newViewsVM(t)
	for _, tc := range []viewFixture{
		{"list_wallets", "wallets_list_sparse.json"},
		{"get_wallet", "wallet_detail_sparse.json"},
		{"wallet_totals", "wallet_totals_sparse.json"},
	} {
		got := modelJSON(t, vm, tc.tool, fixtureJSON(t, tc.fixture))
		requireNoPlaceholder(t, tc.tool, got)
		requireAll(t, tc.tool, got, "Dompet Baru")
	}
}

func TestListViewModelsCarryContent(t *testing.T) {
	vm := newViewsVM(t)
	for _, tc := range []struct {
		tool, fixture string
		want          []string
	}{
		{"list_categories", "category_list.json", []string{"Makan", "Transport", "Gaji"}},
		{"list_periods", "period_list.json", []string{"September 2026", "closed"}},
		{"list_projects", "project_list.json", []string{"Kas Harian", "Acme Group"}},
		{"current_period", "current_period.json", []string{"September 2026", "IDR 120,000"}},
		{"now", "now.json", []string{"Asia/Jakarta", "2026-09-22"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			requireAll(t, tc.tool, modelJSON(t, vm, tc.tool, fixtureJSON(t, tc.fixture)), tc.want...)
			if _, actions := modelFixture(t, vm, tc.tool, tc.fixture); len(actions) != 0 {
				t.Errorf("a read view declares no tool-calling actions, got %v", actions)
			}
		})
	}
}

func TestListViewsSurviveSparsePayloads(t *testing.T) {
	vm := newViewsVM(t)
	for _, tc := range []struct{ tool, fixture, want string }{
		{"list_categories", "category_list_sparse.json", "Makan"},
		{"list_periods", "period_list_sparse.json", "October 2026"},
		{"list_projects", "project_list_sparse.json", "acme"},
		{"current_period", "current_period_sparse.json", "Sep 2026"},
		{"now", "now_sparse.json", "Asia/Jakarta"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			got := modelJSON(t, vm, tc.tool, fixtureJSON(t, tc.fixture))
			requireNoPlaceholder(t, tc.tool, got)
			requireAll(t, tc.tool, got, tc.want)
		})
	}
}

// TestNoMutationEmitsConfirmOutsidePreview is the safety property: only a preview-phase commit control may ask the server to write.
func TestNoMutationEmitsConfirmOutsidePreview(t *testing.T) {
	vm := newViewsVM(t)
	payloads := map[string]string{
		"needs":  `{needs:{needs:[{field:"wallet",reason:"which one?"}]}}`,
		"result": `{result:{id:"x"}}`,
		"empty":  `{warning:"nothing was written"}`,
	}
	for _, tool := range mutationTools {
		for phase, payload := range payloads {
			if got := actionsJSON(t, vm, tool, payload); strings.Contains(got, `"confirm":true`) {
				t.Errorf("%s emits confirm:true in the %s phase: %s", tool, phase, got)
			}
		}
	}
}

func TestEveryMutationOffersCommitOnlyInPreview(t *testing.T) {
	vm := newViewsVM(t)
	for _, tool := range entityMutationTools {
		model, actions := modelInline(t, vm, tool, `{preview:{id:"x",name:"n"}}`)
		if model["phase"] != "preview" || model["commit"] != "commit" {
			t.Errorf("%s preview model = %v", tool, model)
		}
		a, ok := actions["commit"].(map[string]any)
		if !ok || a["tool"] != tool || a["args"].(map[string]any)["confirm"] != true {
			t.Errorf("%s offers no commit control in the preview phase: %v", tool, actions)
		}
	}
}

func TestMutationNeedsModelsCandidatesAndSurvivesNone(t *testing.T) {
	vm := newViewsVM(t)
	model, actions := modelFixture(t, vm, "create_wallet", "create_wallet_needs.json")
	if model["phase"] != "needs" {
		t.Fatalf("phase = %v, want needs", model["phase"])
	}
	field := model["fields"].([]any)[0].(map[string]any)
	if field["select"] != true || field["name"] != "kind" || field["reason"] != "kind is required" {
		t.Errorf("needs field = %v", field)
	}
	if opts := field["options"].([]any); len(opts) != 6 || opts[0] != "cash" || opts[3] != "savings" {
		t.Errorf("needs options = %v", opts)
	}
	if _, ok := actions["edit"]; !ok || model["edit"] != "edit" {
		t.Errorf("needs phase must offer an edit action, got %v", actions)
	}

	bare, _ := modelFixture(t, vm, "create_wallet", "mutation_needs_no_candidates.json")
	bareField := bare["fields"].([]any)[0].(map[string]any)
	if bareField["select"] != false {
		t.Errorf("a need with no candidates has nothing to select from: %v", bareField)
	}
	if !strings.Contains(bareField["reason"].(string), "you belong to no org") {
		t.Errorf("the reason must still be shown: %v", bareField)
	}
}

func TestMutationMapsTargetNeedsToTargetFields(t *testing.T) {
	vm := newViewsVM(t)
	model, _ := modelFixture(t, vm, "create_wallet", "mutation_org_needs.json")
	if got := model["fields"].([]any)[0].(map[string]any)["name"]; got != "target.org" {
		t.Errorf(`an "org" need must submit as target.org, got %v — the server ignores a bare org`, got)
	}
	project, _ := modelInline(t, vm, "record_batch", `{needs:{needs:[{field:"project"}]}}`)
	if got := project["fields"].([]any)[0].(map[string]any)["name"]; got != "target.project" {
		t.Errorf(`a "project" need must submit as target.project, got %v`, got)
	}
}

func TestClosePeriodModelsBothAsymmetricPhases(t *testing.T) {
	vm := newViewsVM(t)
	requireAll(t, "close_period preview is a Snapshot", modelJSON(t, vm, "close_period", fixtureJSON(t, "close_period_preview.json")), "IDR 108,000")
	requireAll(t, "close_period result is a Period", modelJSON(t, vm, "close_period", fixtureJSON(t, "close_period_result.json")), "September 2026")
}

func TestClosePeriodSubjectFallsBackToTheViewsAnswers(t *testing.T) {
	vm := newViewsVM(t)
	if _, err := vm.RunString(`globalThis.capturedArgs = function () { return { period: "per_9" }; };`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	model, _ := modelFixture(t, vm, "close_period", "close_period_preview.json")
	if model["title"] != "Close period: per_9" {
		t.Errorf("title = %v, want the captured period as subject", model["title"])
	}
}

func TestAdjustBalanceNoOpIsASuccessNotAForm(t *testing.T) {
	vm := newViewsVM(t)
	model, actions := modelFixture(t, vm, "adjust_balance", "adjust_balance_noop.json")
	if model["phase"] != "empty" || !strings.Contains(model["message"].(string), "already held that balance") {
		t.Errorf("the empty phase must show the server's warning: %v", model)
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
		var m map[string]any
		if err := json.Unmarshal([]byte(fixtureJSON(t, e.Name())), &m); err != nil {
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
	vm := newViewsVM(t)
	model, actions := modelFixture(t, vm, "record_batch", "record_batch_preview.json")
	batch := model["batch"].(map[string]any)
	rows := batch["rows"].([]any)
	if rows[0].(map[string]any)["row"] != "Row 1" {
		t.Errorf("BatchOutcome.index is omitted for row 0; the first row must be numbered from it: %v", rows[0])
	}
	refused := rows[2].(map[string]any)
	if refused["failed"] != true || refused["code"] != "YSK404" || !strings.Contains(refused["error"].(string), "not found") {
		t.Errorf("a refused row must carry its errorCode and message: %v", refused)
	}
	if batch["label"] != "2 of 3 rows are writable" {
		t.Errorf("label = %v", batch["label"])
	}
	if _, ok := actions["commit"]; !ok {
		t.Errorf("a batch preview must offer a commit, got %v", actions)
	}

	res, _ := modelFixture(t, vm, "record_batch", "record_batch_result.json")
	second := res["batch"].(map[string]any)["rows"].([]any)[1].(map[string]any)
	if second["failed"] != true || second["code"] != "refused" || second["error"] != "period is closed" {
		t.Errorf("a row carrying error with no errorCode must still be marked failed: %v", second)
	}
}

func TestSeedZeroOffersNoCommit(t *testing.T) {
	vm := newViewsVM(t)
	zero, actions := modelFixture(t, vm, "seed_default_categories", "seed_zero.json")
	if len(actions) != 0 || zero["phase"] != "empty" {
		t.Errorf("nothing to seed means nothing to commit, got %v / %v", zero, actions)
	}
	prev, prevActions := modelFixture(t, vm, "seed_default_categories", "seed_preview.json")
	if !strings.Contains(prev["note"].(string), "12") {
		t.Errorf("seed preview must show the count: %v", prev)
	}
	if _, ok := prevActions["commit"]; !ok {
		t.Errorf("seed preview must offer a commit, got %v", prevActions)
	}
}

func TestBulkViewsTolerateAScalarPreview(t *testing.T) {
	vm := newViewsVM(t)
	for _, tool := range []string{"record_batch", "seed_default_categories"} {
		requireNoPlaceholder(t, tool, modelJSON(t, vm, tool, `{preview:{id:"x"}}`))
	}
}

func TestPreviewsAndBatchRowsCarryTheCivilDate(t *testing.T) {
	vm := newViewsVM(t)
	for _, tc := range []struct{ tool, fixture, civil, utc string }{
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
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			got := modelJSON(t, vm, tc.tool, fixtureJSON(t, tc.fixture))
			if !strings.Contains(got, `"date":"`+tc.civil+`"`) {
				t.Errorf("%s must carry the project-timezone day %s:\n%s", tc.fixture, tc.civil, got)
			}
			if tc.utc != "" && strings.Contains(got, tc.utc) {
				t.Errorf("%s leaked the UTC day %s from occurredAt:\n%s", tc.fixture, tc.utc, got)
			}
		})
	}
}

// TestNoViewBindsAnActionOnAForm: a handler on the <form> fires on any click inside it, before the user has chosen.
func TestNoViewBindsAnActionOnAForm(t *testing.T) {
	formClick := regexp.MustCompile(`<form[^>]*@click`)
	for _, p := range scriptParts {
		if strings.HasPrefix(p, "src/views/") && formClick.MatchString(mustRead(t, p)) {
			t.Errorf("%s binds @click on a <form>", p)
		}
	}
}
