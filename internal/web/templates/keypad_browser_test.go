package templates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"altalune.id/yasaku/internal/web"
)

type keypadCase struct {
	Dec  string `json:"dec"`
	Exp  int    `json:"exp"`
	Raw  string `json:"raw"`
	Want string `json:"want"`
}

const keypadProbe = `
window.addEventListener("load", function () {
  var out = window.KEYPAD_CASES.map(function (c) {
    var form = document.createElement("form");
    form.setAttribute("data-tx-sheet", "");
    var input = document.createElement("input");
    input.setAttribute("data-keypad-input", "");
    input.dataset.decimal = c.dec;
    input.dataset.exponent = String(c.exp);
    input.dataset.currency = "USD";
    input.value = c.raw;
    form.appendChild(input);
    document.body.appendChild(form);
    form.addEventListener("submit", function (e) { e.preventDefault(); });
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    return input.value;
  });
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

// TestKeypad_SubmitsOnlyWhatTheServerWouldAccept mirrors the ParseLocale table: an expression keypad.js cannot read the way the server would stays raw, so the server refuses it.
func TestKeypad_SubmitsOnlyWhatTheServerWouldAccept(t *testing.T) {
	const idID, enUS = ",", "."
	cases := []keypadCase{
		{Dec: idID, Exp: 0, Raw: "12.500+5", Want: "12505"},
		{Dec: enUS, Exp: 0, Raw: "12.500+5", Want: "12505"},
		{Dec: enUS, Exp: 0, Raw: "12,500+5", Want: "12505"},
		{Dec: idID, Exp: 2, Raw: "12.500,50+1", Want: "12501,50"},
		{Dec: enUS, Exp: 2, Raw: "12,500.50+1", Want: "12501.50"},
		{Dec: idID, Exp: 2, Raw: "12,50+0,25", Want: "12,75"},
		{Dec: idID, Exp: 2, Raw: "12.500+1", Want: "12501"},
		{Dec: enUS, Exp: 2, Raw: "10÷3", Want: "3.33"},
		{Dec: enUS, Exp: 2, Raw: "19.99×3", Want: "59.97"},
		{Dec: enUS, Exp: 0, Raw: "100+", Want: "100"},
		{Dec: idID, Exp: 2, Raw: "1.5+1", Want: "1.5+1"},
		{Dec: enUS, Exp: 0, Raw: "12,50+5", Want: "12,50+5"},
		{Dec: idID, Exp: 0, Raw: "1.2.34+1", Want: "1.2.34+1"},
		{Dec: enUS, Exp: 2, Raw: "12.500+1", Want: "12.500+1"},
		{Dec: enUS, Exp: 2, Raw: "1,234.5,6+1", Want: "1,234.5,6+1"},
		{Dec: enUS, Exp: 2, Raw: "1.234,5+1", Want: "1.234,5+1"},
		{Dec: enUS, Exp: 0, Raw: "1,23+1", Want: "1,23+1"},
		{Dec: enUS, Exp: 0, Raw: "12.500,50+1", Want: "12.500,50+1"},
		{Dec: enUS, Exp: 2, Raw: "5−10", Want: "5−10"},
		{Dec: enUS, Exp: 0, Raw: "-5+1", Want: "-5+1"},
		{Dec: enUS, Exp: 0, Raw: "8÷0", Want: "8÷0"},
		{Dec: idID, Exp: 2, Raw: "0.500+1", Want: "0.500+1"},
		{Dec: idID, Exp: 0, Raw: "0.500+1", Want: "0.500+1"},
		{Dec: enUS, Exp: 2, Raw: "0.50+1", Want: "1.50"},
	}
	got := runKeypad(t, cases)
	if len(got) != len(cases) {
		t.Fatalf("got %d results for %d cases: %v", len(got), len(cases), got)
	}
	for i, c := range cases {
		if got[i] != c.Want {
			t.Errorf("dec %q exp %d: %q submitted as %q, want %q", c.Dec, c.Exp, c.Raw, got[i], c.Want)
		}
	}
}

func runKeypad(t *testing.T, cases []keypadCase) []string {
	t.Helper()
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/keypad.js"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html lang="en-US"><body><pre id="out"></pre><script>window.KEYPAD_CASES = ` + string(data) +
		`;</script><script>` + keypadProbe + `</script><script>` + string(script) + `</script></body></html>`
	return runProbe(t, page)
}

const rowActionsProbe = `
document.addEventListener("click", function (e) { if (e.target.closest("a")) e.preventDefault(); });
window.addEventListener("load", function () {
  document.querySelectorAll("[data-row-edit]")[1].click();
  var d = document.getElementById("tx-row-actions");
  var out = [d.querySelector('[data-row-target="edit"]').getAttribute("href"),
    d.querySelector('[data-row-target="duplicate"]').getAttribute("href"),
    new URL(d.querySelector('[data-row-target="delete"]').action).pathname,
    d.querySelector("#tx-row-actions-title").textContent];
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

// TestRowActions_TheSharedDialogTargetsThePressedRow drives swipe.js: the one actions dialog points at the row whose opener was pressed.
func TestRowActions_TheSharedDialogTargetsThePressedRow(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}, CurrentPath: "/orgs/acme/projects/main/transactions"}
	rows := []TxRow{{ID: "a1", Kind: "expense", Note: "Bakso", Editable: true}, {ID: "b2", Kind: "expense", Note: "Kopi", Editable: true}}
	body := render(t, TxRowsPage(d, TxListView{ProjectSlug: "main", Rows: rows})) + render(t, TxRowActions(d))
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/swipe.js"))
	if err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html lang="en-US"><body>` + body + `<pre id="out"></pre><script>` + string(script) +
		`</script><script>` + rowActionsProbe + `</script></body></html>`
	got := runProbe(t, page)
	want := []string{"/orgs/acme/projects/main/transactions/b2/edit", "/orgs/acme/projects/main/transactions/new?from=b2", "/orgs/acme/projects/main/transactions/b2/delete", "Kopi"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("dialog targets = %q, want %q", got, want)
	}
}

func runProbe(t *testing.T, page string, flags ...string) []string {
	t.Helper()
	bin := localtimeBrowser(t)
	file := filepath.Join(t.TempDir(), "probe.html")
	if err := os.WriteFile(file, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	args := append([]string{"--headless", "--disable-gpu", "--no-sandbox"}, flags...)
	args = append(args, "--virtual-time-budget=5000", "--dump-dom", "file://"+file)
	out, err := exec.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		t.Fatalf("run %s: %v", bin, err)
	}
	m := localtimeOut.FindSubmatch(out)
	if m == nil {
		t.Fatalf("no #out payload:\n%s", out)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(m[1])))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

const recentCompactProbe = `
window.addEventListener("load", function () {
  var out = Array.prototype.map.call(document.querySelectorAll("[data-compact]"), function (el) { return el.textContent; });
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

// TestRecent_CompactAmountsSpaceLikeTheServer keeps a no-break space after a letter symbol and none after a sign symbol.
func TestRecent_CompactAmountsSpaceLikeTheServer(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/recent.js"))
	if err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html lang="en-US"><body><pre id="out"></pre><form data-tx-sheet>` +
		`<span data-compact="25000" data-symbol="IDR">x</span><span data-compact="186500" data-symbol="Rp">x</span><span data-compact="25000" data-symbol="$">x</span>` +
		`</form><script>` + recentCompactProbe + `</script><script>` + string(script) + `</script></body></html>`
	got := runProbe(t, page)
	want := []string{"IDR 25K", "Rp 186.5K", "$25K"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("compact amounts = %q, want %q", got, want)
	}
}
