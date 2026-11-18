package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/money"
)

func sheetFormView() TxFormView {
	v := TxFormView{
		ProjectSlug: "main",
		Kind:        "expense",
		Symbol:      "Rp",
		Currency:    "IDR",
		Decimal:     ",",
		Periods:     []TxPeriodOption{{ID: "p1", Label: "Oktober"}, {ID: "p2", Label: "November", Selected: true}},
		Wallets: []TxWalletOption{
			{ID: "w1", Name: "Tunai", Balance: money.New(500_000, money.IDR), Selected: true, Symbol: "Rp", Currency: "IDR"},
			{ID: "w2", Name: "Rekening bank", Balance: money.New(0, money.IDR), Symbol: "Rp", Currency: "IDR"},
		},
	}
	for _, name := range []string{"Makan & Minum", "Tagihan & Utilitas", "Pulsa & Internet", "Transportasi", "Belanja", "Kesehatan", "Hiburan", "Pendidikan", "Rumah Tangga", "Lainnya"} {
		v.Categories = append(v.Categories, TxCategoryOption{ID: name, Name: name, Icon: "utensils", Kind: "expense"})
	}
	return v
}

func runLayoutProbe(t *testing.T, width, height int, body, probe string) []string {
	t.Helper()
	css, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/app.css"))
	if err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html lang="id-ID" data-theme="light"><head><meta name="viewport" content="width=device-width, initial-scale=1"><style>` +
		string(css) + `*,::backdrop{transition:none!important;animation:none!important}</style></head><body><div style="width:` + strconv.Itoa(width) + `px">` + body + `</div><pre id="out"></pre><script>` +
		probe + `</script></body></html>`
	// NOTE: headless Chrome will not open a window narrower than 500px, so phone widths come from the box.
	window := fmt.Sprintf("--window-size=%d,%d", max(width, 500), height)
	return runProbe(t, page, "--hide-scrollbars", window)
}

const sheetFitProbe = `
window.addEventListener("load", function () {
  var dlg = document.getElementById("tx-add-dialog");
  document.querySelector("[data-keypad]").hidden = false;
  dlg.showModal();
  var vh = window.innerHeight, out = [];
  function inView(name, el) {
    var r = el.getBoundingClientRect();
    if (r.top < 0 || r.bottom > vh) out.push(name + " outside the viewport: " + Math.round(r.top) + ".." + Math.round(r.bottom) + " of " + vh);
  }
  inView("dialog box", dlg.querySelector(".modal-box"));
  inView("keypad", dlg.querySelector("[data-keypad]"));
  inView("save", dlg.querySelector("button[type=submit]:not([name])"));
  var fields = dlg.querySelector("[data-tx-fields]");
  var scroller = getComputedStyle(fields).overflowY === "auto" ? fields : dlg.querySelector(".modal-box > section");
  scroller.scrollTop = scroller.scrollHeight;
  inView("wallet select after scrolling", dlg.querySelector("#tx-wallet"));
  inView("save after scrolling", dlg.querySelector("button[type=submit]:not([name])"));
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

// TestTxSheet_KeepsSaveInView keeps the keypad and Save on screen: on desktop the field column scrolls, on a phone the keypad column sticks.
func TestTxSheet_KeepsSaveInView(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}, CurrentPath: "/orgs/acme/projects/main/overview"}
	body := render(t, TxSheet(d, sheetFormView()))
	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"desktop 1280x800", 1280, 800},
		{"phone 390x844", 390, 844},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runLayoutProbe(t, tc.width, tc.height, body, sheetFitProbe); len(got) > 0 {
				t.Fatalf("%s:\n%s", tc.name, strings.Join(got, "\n"))
			}
		})
	}
}

const labelFitProbe = `
window.addEventListener("load", function () {
  var out = [];
  document.querySelectorAll("[data-label-fit]").forEach(function (el) {
    var text = el.textContent.trim(), node = el.firstChild, lines = {}, at = 0;
    var all = document.createRange();
    all.selectNodeContents(el);
    Array.prototype.forEach.call(all.getClientRects(), function (r) { lines[Math.round(r.top)] = 1; });
    if (Object.keys(lines).length > 2) out.push(text + ": more than two lines");
    var titled = el.closest("[title]") && el.closest("[title]").title === text;
    if ((!titled && el.scrollWidth > el.clientWidth + 1) || el.scrollHeight > el.clientHeight + 1) out.push(text + ": clipped");
    text.split(" ").forEach(function (word) {
      var i = node.textContent.indexOf(word, at), r = document.createRange();
      at = i + word.length;
      r.setStart(node, i);
      r.setEnd(node, at);
      var tops = {};
      Array.prototype.forEach.call(r.getClientRects(), function (q) { tops[Math.round(q.top)] = 1; });
      if (Object.keys(tops).length > 1) out.push(text + ": breaks inside " + word);
    });
  });
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

// TestCategoryLabels_WrapAtWordsWithoutClipping checks the sheet grid and Home Quick add on a phone and in the desktop side column; a word wider than its tile may truncate only when the tile's title carries the full name.
func TestCategoryLabels_WrapAtWordsWithoutClipping(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}, CurrentPath: "/orgs/acme/projects/main/overview"}
	form := sheetFormView()
	home := YasakuOverviewView{ProjectSlug: "main", HasPeriod: true}
	for _, c := range form.Categories[:8] {
		home.QuickAdd = append(home.QuickAdd, YasakuQuickAdd{ID: c.ID, Name: c.Name, Icon: c.Icon})
	}
	sheet := render(t, TransactionForm(d, txInDialog(form)))
	page := `<main class="mx-auto w-full max-w-6xl px-4 lg:px-8">` + render(t, homeBody(d, home)) + `</main>`
	for _, tc := range []struct {
		name  string
		width int
		body  string
	}{
		{"sheet at 390", 390, sheet},
		{"home at 390", 390, page},
		{"home beside the 1280 sidebar", 1024, page},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := runLayoutProbe(t, tc.width, 900, tc.body, labelFitProbe); len(got) > 0 {
				t.Fatalf("%s:\n%s", tc.name, strings.Join(got, "\n"))
			}
		})
	}
}

const moreFitProbe = `
window.addEventListener("load", function () {
  var out = [];
  document.querySelectorAll(".list-row .truncate").forEach(function (el) {
    if (el.scrollWidth > el.clientWidth + 1) out.push(el.textContent.trim() + ": truncated");
  });
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

// TestMorePage_RowsFitOneLineOnAPhone keeps every More title and description whole at 390 in every locale.
func TestMorePage_RowsFitOneLineOnAPhone(t *testing.T) {
	for _, loc := range []i18n.Locale{i18n.EnUS, i18n.IdID, i18n.MsMY, i18n.JaJP, i18n.ArSA} {
		t.Run(string(loc), func(t *testing.T) {
			d := shellData("more", false)
			d.Translator = i18n.NewEmbeddedBundle(loc).For(loc)
			body := `<main class="px-4">` + render(t, MorePage(d)) + `</main>`
			if got := runLayoutProbe(t, 390, 900, body, moreFitProbe); len(got) > 0 {
				t.Fatalf("%s at 390:\n%s", loc, strings.Join(got, "\n"))
			}
		})
	}
}

const sheetBehaviorProbe = `
window.addEventListener("load", function () {
  var out = [];
  var dlg = document.getElementById("tx-add-dialog");
  dlg.showModal();
  var form = dlg.querySelector("form"), input = form.querySelector("#tx-amount"), save = form.querySelector("[data-tx-save]");
  var line = form.querySelector("[data-amount-text]");
  input.focus();
  if (getComputedStyle(input).caretColor !== "rgba(0, 0, 0, 0)") out.push("native caret visible");
  if (form.querySelector("[data-amount-caret]")) out.push("a custom caret element exists");
  if (getComputedStyle(line).borderBottomWidth === "0px") out.push("no underline under the amount");
  var empty = line.getBoundingClientRect().width;
  if (!save.disabled) out.push("Save enabled with no amount");
  ["5", "5", "000"].forEach(function (k) { form.querySelector('[data-key="' + k + '"]').click(); });
  if (save.disabled) out.push("Save disabled after digits");
  if (line.textContent !== input.value) out.push("underline text " + line.textContent + " does not follow " + input.value);
  if (line.getBoundingClientRect().width <= empty * 3) out.push("underline does not grow with the digits");
  if (line.getBoundingClientRect().width >= input.getBoundingClientRect().width) out.push("underline spans the whole field");
  var tiles = form.querySelectorAll("[data-cat-kind=expense] label:has([name=category_id])");
  var shown = function () { return Array.prototype.filter.call(tiles, function (t) { return t.getClientRects().length > 0; }).length; };
  if (shown() !== 7) out.push("folded grid shows " + shown() + " tiles, want 7");
  form.querySelector("[data-cat-kind=expense] [data-cat-more]").click();
  if (shown() !== tiles.length) out.push("More shows " + shown() + " of " + tiles.length + " tiles");
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

// TestTxSheet_UnderlineSaveAndMoreBehave drives keypad.js on the rendered sheet: no caret shows, the underline follows the digits, Save waits for an amount, and More unfolds the grid.
func TestTxSheet_UnderlineSaveAndMoreBehave(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}, CurrentPath: "/orgs/acme/projects/main/overview"}
	body := render(t, TxSheet(d, sheetFormView()))
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/keypad.js"))
	if err != nil {
		t.Fatal(err)
	}
	body += "<script>" + string(script) + "</script>"
	if got := runLayoutProbe(t, 1280, 800, body, sheetBehaviorProbe); len(got) > 0 {
		t.Fatal(strings.Join(got, "\n"))
	}
}

const toWalletRequiredProbe = `
window.addEventListener("load", function () {
  var out = [], form = document.querySelector("form[data-tx-sheet]"), to = form.querySelector("#tx-to-wallet");
  function pick(kind) {
    var r = form.querySelector('[name=kind][value="' + kind + '"]');
    r.checked = true;
    r.dispatchEvent(new Event("change", { bubbles: true }));
  }
  if (to.required) out.push("To wallet required on expense");
  pick("transfer");
  if (!to.required) out.push("To wallet not required on transfer");
  pick("income");
  if (to.required) out.push("To wallet still required on income");
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

// TestTxSheet_ToWalletRequiredFollowsKind drives keypad.js: the hidden To wallet never blocks an expense, and Transfer requires it.
func TestTxSheet_ToWalletRequiredFollowsKind(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}, CurrentPath: "/orgs/acme/projects/main/overview"}
	body := render(t, TxSheet(d, sheetFormView()))
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/keypad.js"))
	if err != nil {
		t.Fatal(err)
	}
	body += "<script>" + string(script) + "</script>"
	if got := runLayoutProbe(t, 1280, 800, body, toWalletRequiredProbe); len(got) > 0 {
		t.Fatal(strings.Join(got, "\n"))
	}
}
