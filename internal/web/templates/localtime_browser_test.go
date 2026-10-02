package templates

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

// NOTE: localtime.js needs a real Intl, so it is browser-tested; set LOCALTIME_BROWSER to a Chrome binary or these skip.
func localtimeBrowser(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("LOCALTIME_BROWSER"); bin != "" {
		return bin
	}
	for _, bin := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/usr/bin/google-chrome",
		"/usr/bin/chromium",
	} {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	t.Skip("no Chrome found; set LOCALTIME_BROWSER to run the localtime.js browser test")
	return ""
}

const localtimeProbe = `
var navigated = 0;
document.addEventListener("click", function (e) {
  if (!e.target.closest || !e.target.closest("a")) return;
  if (!e.defaultPrevented) navigated++;
  e.preventDefault();
});
window.addEventListener("load", function () {
  var rows = [];
  document.querySelectorAll("time").forEach(function (el) {
    var btn = el.closest("[data-dt-trigger]");
    var pop = btn ? btn.popoverTargetElement : null;
    var row = { text: el.textContent, armed: !!btn, pop: pop ? pop.textContent : "", described: !!pop && btn.getAttribute("aria-describedby") === pop.id };
    var r = el.getBoundingClientRect();
    navigated = 0;
    document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2).click();
    row.click = navigated ? "link" : pop && pop.matches(":popover-open") ? "popover" : "none";
    if (pop && pop.matches(":popover-open")) pop.hidePopover();
    rows.push(row);
  });
  var hint = document.querySelector("[data-tz-hint]");
  var out = { rows: rows, hint: hint && !hint.hidden ? hint.textContent : "" };
  document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
});
`

type localtimeRow struct {
	Text      string `json:"text"`
	Armed     bool   `json:"armed"`
	Pop       string `json:"pop"`
	Described bool   `json:"described"`
	Click     string `json:"click"`
}

type localtimeReport struct {
	Rows []localtimeRow `json:"rows"`
	Hint string         `json:"hint"`
}

var localtimeOut = regexp.MustCompile(`(?s)<pre id="out">(.*?)</pre>`)

func runLocaltime(t *testing.T, browserTZ string, body string) localtimeReport {
	t.Helper()
	bin := localtimeBrowser(t)
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/localtime.js"))
	if err != nil {
		t.Fatal(err)
	}
	css, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/app.css"))
	if err != nil {
		t.Fatal(err)
	}
	page := `<!doctype html><html lang="en-US"><head><style>` + string(css) + `</style></head><body>` + body +
		`<pre id="out"></pre><script>` + localtimeProbe + `</script><script>` + string(script) + `</script></body></html>`
	dir := t.TempDir()
	file := filepath.Join(dir, "localtime.html")
	if err := os.WriteFile(file, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--headless", "--disable-gpu", "--no-sandbox",
		"--virtual-time-budget=5000", "--dump-dom", "file://"+file)
	cmd.Env = append(os.Environ(), "TZ="+browserTZ)
	out, err := cmd.Output()
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
	var rep localtimeReport
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), " ", " ")), &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

func renderLedgerPage(t *testing.T, ledgerTZ string) string {
	t.Helper()
	loc, err := time.LoadLocation(ledgerTZ)
	if err != nil {
		t.Fatal(err)
	}
	d := web.LayoutData{
		TimeZone:   loc,
		Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS),
		ActiveOrg:  &web.ActiveOrg{Slug: "acme"},
	}
	oct := time.Date(2026, 10, 2, 9, 55, 0, 0, time.UTC)
	nov := time.Date(2026, 11, 1, 9, 55, 0, 0, time.UTC)
	row := TxRow{
		ID: "tx1", Kind: "expense", Note: "Bakso", Editable: true,
		Day: civil.Date{Year: 2026, Month: time.October, Day: 1}, OccurredAt: time.Date(2026, 9, 30, 22, 0, 0, 0, time.UTC),
	}
	return `<p>` + render(t, DateTime(d, oct)) + `</p><p>` + render(t, DateTime(d, nov)) + `</p>` +
		render(t, TxRowsPage(d, TxListView{ProjectSlug: "main", Rows: []TxRow{row}})) +
		render(t, LedgerTimeFooter(d))
}

func TestLocaltime_FormatsInTheBrowserZoneAndPopsTheLedgerZone(t *testing.T) {
	cases := []struct {
		name, browser, ledger string
		want                  []localtimeRow
		hint                  string
	}{
		{
			name: "jakarta browser, london ledger", browser: "Asia/Jakarta", ledger: "Europe/London",
			want: []localtimeRow{
				{Text: "Oct 2, 2026, 4:55 PM", Armed: true, Described: true, Click: "popover", Pop: "Oct 2, 2026, 10:55 AM · Ledger time (Europe/London, GMT+1)"},
				{Text: "Nov 1, 2026, 4:55 PM", Armed: true, Described: true, Click: "popover", Pop: "Nov 1, 2026, 9:55 AM · Ledger time (Europe/London, GMT+0)"},
				{Text: "Oct 1", Armed: true, Described: true, Click: "popover", Pop: "Recorded Oct 1, 2026, 5:00 AM · your time (GMT+7)"},
			},
			hint: "Times in your timezone (GMT+7) · Ledger: Europe/London (GMT+1)",
		},
		{
			name: "london browser, jakarta ledger", browser: "Europe/London", ledger: "Asia/Jakarta",
			want: []localtimeRow{
				{Text: "Oct 2, 2026, 10:55 AM", Armed: true, Described: true, Click: "popover", Pop: "Oct 2, 2026, 4:55 PM · Ledger time (Asia/Jakarta, GMT+7)"},
				{Text: "Nov 1, 2026, 9:55 AM", Armed: true, Described: true, Click: "popover", Pop: "Nov 1, 2026, 4:55 PM · Ledger time (Asia/Jakarta, GMT+7)"},
				{Text: "Oct 1", Armed: true, Described: true, Click: "popover", Pop: "Recorded Sep 30, 2026, 11:00 PM · your time (GMT+1)"},
			},
			hint: "Times in your timezone (GMT+1) · Ledger: Asia/Jakarta (GMT+7)",
		},
		{
			name: "same zone, no popover", browser: "Asia/Jakarta", ledger: "Asia/Jakarta",
			want: []localtimeRow{
				{Text: "Oct 2, 2026, 4:55 PM", Click: "none"},
				{Text: "Nov 1, 2026, 4:55 PM", Click: "none"},
				{Text: "Oct 1", Click: "link"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := runLocaltime(t, tc.browser, renderLedgerPage(t, tc.ledger))
			if len(rep.Rows) != len(tc.want) {
				t.Fatalf("rows = %+v, want %+v", rep.Rows, tc.want)
			}
			for i, want := range tc.want {
				if rep.Rows[i] != want {
					t.Errorf("row %d = %+v, want %+v", i, rep.Rows[i], want)
				}
			}
			if rep.Hint != tc.hint {
				t.Errorf("hint = %q, want %q", rep.Hint, tc.hint)
			}
		})
	}
}
