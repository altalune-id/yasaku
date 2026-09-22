package ui

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestDumpPreview renders every registered view against its fixture into one static page so
// layout can be checked without a host. Opt in with YASAKU_UI_PREVIEW=<path>.
func TestDumpPreview(t *testing.T) {
	path := os.Getenv("YASAKU_UI_PREVIEW")
	if path == "" {
		t.Skip("set YASAKU_UI_PREVIEW=path to write the rendered preview")
	}
	cases := []struct{ tool, fixture string }{
		{"period_report", "period_report.json"},
		{"period_report (sparse)", "period_report_sparse.json"},
		{"cashflow_report", "cashflow_report.json"},
		{"preview_close", "preview_close.json"},
		{"list_recent_tx", "tx_list.json"},
		{"list_wallets", "wallets_list.json"},
		{"get_wallet", "wallet_detail.json"},
		{"wallet_totals", "wallet_totals.json"},
		{"list_categories", "category_list.json"},
		{"list_periods", "period_list.json"},
		{"list_projects", "project_list.json"},
		{"current_period", "current_period.json"},
		{"now", "now.json"},
		{"create_wallet (needs)", "create_wallet_needs.json"},
		{"create_wallet (preview)", "create_wallet_preview.json"},
		{"create_wallet (result)", "create_wallet_result.json"},
		{"record_expense", "record_expense_preview.json"},
		{"record_transfer", "record_transfer_preview.json"},
		{"delete_tx", "delete_tx_result.json"},
		{"close_period (preview)", "close_period_preview.json"},
		{"close_period (result)", "close_period_result.json"},
		{"reopen_period", "reopen_period_preview.json"},
		{"create_category", "create_category_preview.json"},
		{"adjust_balance (no-op)", "adjust_balance_noop.json"},
		{"record_batch", "record_batch_preview.json"},
		{"seed_default_categories", "seed_preview.json"},
	}

	vm := newJSVM(t)
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>yasaku MCP views</title><style>\n")
	b.WriteString(mustRead(t, "app.css"))
	b.WriteString("\nbody{padding:24px;background:light-dark(#fff,#111)}")
	b.WriteString("\n.pv{max-width:520px;margin:0 0 28px;border:1px dashed #888;border-radius:14px}")
	b.WriteString("\n.pv h2{font:600 12px ui-monospace,monospace;margin:0;padding:8px 12px;border-bottom:1px dashed #888;opacity:.7}")
	b.WriteString("\n</style></head><body>\n")

	for _, c := range cases {
		raw, err := fixtures.ReadFile("testdata/" + c.fixture)
		if err != nil {
			t.Fatalf("read %s: %v", c.fixture, err)
		}
		tool := strings.Fields(c.tool)[0]
		v, err := vm.RunString("renderTool(" + strconv.Quote(tool) + ", " + string(raw) + ").html")
		if err != nil {
			t.Fatalf("render %s: %v", c.tool, err)
		}
		b.WriteString("<section class=\"pv\"><h2>" + c.tool + "</h2>")
		b.WriteString(v.String())
		b.WriteString("</section>\n")
	}
	b.WriteString("</body></html>\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %s", path)
}
