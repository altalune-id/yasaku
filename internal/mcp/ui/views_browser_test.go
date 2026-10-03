package ui

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
)

const viewsDriver = `
const reports = [];
for (const c of __cases) {
  const host = document.createElement("div");
  document.getElementById("root").appendChild(host);
  const app = document.createElement("yasaku-app");
  host.appendChild(app);
  const events = [];
  app.onaction = (d) => events.push({
    id: d.id,
    form: !!d.form,
    fields: d.form ? Array.from(d.form.querySelectorAll("[name]")).map((f) => f.getAttribute("name")) : [],
  });
  app.view = renderTool(c.tool, c.data);
  app.status = "view";
  await app.updateComplete;
  const inner = Array.from(app.renderRoot.children).find((e) => e.tagName.startsWith("YASAKU-"));
  if (!inner) { reports.push({ label: c.label, tag: "", outer: app.renderRoot.innerHTML }); continue; }
  await inner.updateComplete;
  const r = inner.renderRoot;
  const all = Array.from(r.querySelectorAll("*"));
  const svgKids = Array.from(r.querySelectorAll("svg *"));
  const rep = {
    label: c.label,
    tag: inner.tagName.toLowerCase(),
    scoped: !!inner.shadowRoot,
    text: r.textContent,
    inner: r.innerHTML,
    images: r.querySelectorAll("img").length,
    scripts: r.querySelectorAll("script").length,
    circles: r.querySelectorAll("circle").length,
    rects: r.querySelectorAll("rect").length,
    foreign: svgKids.filter((e) => e.namespaceURI !== "http://www.w3.org/2000/svg").length,
    handlers: all.filter((e) => Array.from(e.attributes).some((a) => a.name.startsWith("on"))).length,
    strokes: Array.from(r.querySelectorAll("circle[stroke]")).map((e) => e.getAttribute("stroke")),
    styles: all.filter((e) => e.hasAttribute("style")).map((e) => e.getAttribute("style")),
    positions: all.filter((e) => getComputedStyle(e).position === "fixed").length,
    fields: Array.from(r.querySelectorAll("[name]")).map((e) => e.getAttribute("name")),
    buttons: Array.from(r.querySelectorAll("button")).map((b) => b.textContent.trim()),
    clicked: [],
    submitted: [],
    submitPrevented: false,
  };
  const button = r.querySelector("button");
  if (button) { button.click(); rep.clicked = events.splice(0); }
  const form = r.querySelector("form");
  if (form) {
    form.addEventListener("submit", (ev) => { rep.submitPrevented = ev.defaultPrevented; });
    form.requestSubmit();
    rep.submitted = events.splice(0);
  }
  reports.push(rep);
  host.remove();
}
document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(reports))));
`

type viewEvent struct {
	ID     string   `json:"id"`
	Form   bool     `json:"form"`
	Fields []string `json:"fields"`
}

type viewReport struct {
	Label           string      `json:"label"`
	Tag             string      `json:"tag"`
	Outer           string      `json:"outer"`
	Scoped          bool        `json:"scoped"`
	Text            string      `json:"text"`
	Inner           string      `json:"inner"`
	Images          int         `json:"images"`
	Scripts         int         `json:"scripts"`
	Circles         int         `json:"circles"`
	Rects           int         `json:"rects"`
	Foreign         int         `json:"foreign"`
	Handlers        int         `json:"handlers"`
	Strokes         []string    `json:"strokes"`
	Styles          []string    `json:"styles"`
	Positions       int         `json:"positions"`
	Fields          []string    `json:"fields"`
	Buttons         []string    `json:"buttons"`
	Clicked         []viewEvent `json:"clicked"`
	Submitted       []viewEvent `json:"submitted"`
	SubmitPrevented bool        `json:"submitPrevented"`
}

type viewCase struct {
	Label string          `json:"label"`
	Tool  string          `json:"tool"`
	Data  json.RawMessage `json:"data"`
}

func renderViewsInBrowser(t *testing.T, cases []viewCase) map[string]viewReport {
	t.Helper()
	bin := findBrowser(t)
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatalf("marshal cases: %v", err)
	}

	var doc strings.Builder
	doc.WriteString("<!doctype html><html><head><style>")
	doc.WriteString(read("app.css"))
	doc.WriteString("</style></head><body><div id=\"root\"></div><pre id=\"out\"></pre>\n")
	doc.WriteString("<script type=\"module\">")
	doc.WriteString(exposeExports(mustRead(t, litPart), "__lit"))
	doc.WriteString("</script>\n<script type=\"module\">\n")
	for _, p := range scriptParts {
		if domParts[p] {
			continue
		}
		doc.WriteString(mustRead(t, p))
		doc.WriteString("\n")
	}
	doc.WriteString("const __cases = ")
	doc.WriteString(strings.ReplaceAll(string(data), "</", `<\/`))
	doc.WriteString(";\n")
	doc.WriteString(viewsDriver)
	doc.WriteString("</script></body></html>")

	page := filepath.Join(t.TempDir(), "views.html")
	if err := os.WriteFile(page, []byte(doc.String()), 0o600); err != nil {
		t.Fatalf("write harness: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"--headless", "--disable-gpu", "--no-sandbox",
		"--virtual-time-budget=10000", "--dump-dom", "file://"+page).Output()
	if err != nil {
		t.Fatalf("run %s: %v", bin, err)
	}
	m := dumpedOut.FindSubmatch(out)
	if m == nil || len(strings.TrimSpace(string(m[1]))) == 0 {
		t.Fatalf("the harness produced no #out payload:\n%s", out)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(m[1])))
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var reps []viewReport
	if err := json.Unmarshal(raw, &reps); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	byLabel := make(map[string]viewReport, len(reps))
	for _, r := range reps {
		byLabel[r.Label] = r
	}
	if len(byLabel) != len(cases) {
		t.Fatalf("rendered %d cases, want %d", len(byLabel), len(cases))
	}
	return byLabel
}

func fixtureCases(t *testing.T) []viewCase {
	t.Helper()
	cases := make([]viewCase, 0, len(viewFixtures))
	for _, f := range viewFixtures {
		cases = append(cases, viewCase{Label: f.tool + "/" + f.fixture, Tool: f.tool, Data: json.RawMessage(fixtureJSON(t, f.fixture))})
	}
	return cases
}

func TestRenderLayerRendersEveryYasakuViewInAShadowRoot(t *testing.T) {
	cases := fixtureCases(t)
	reps := renderViewsInBrowser(t, cases)
	for _, c := range cases {
		r := reps[c.Label]
		if r.Tag == "" || !r.Scoped {
			t.Errorf("%s did not mount a scoped yasaku-* element:\n%s", c.Label, r.Outer)
			continue
		}
		if strings.TrimSpace(r.Text) == "" {
			t.Errorf("%s rendered no text", c.Label)
		}
		requireNoPlaceholder(t, c.Label, r.Text)
		if r.Foreign != 0 {
			t.Errorf("%s has %d chart children outside the SVG namespace", c.Label, r.Foreign)
		}
	}
	for label, want := range map[string]string{
		"period_report/period_report.json":       "Makan",
		"list_recent_tx/tx_list.json":            "Food & Drinks",
		"wallet_totals/wallet_totals.json":       "Spendable",
		"close_period/close_period_result.json":  "September 2026",
		"record_batch/record_batch_preview.json": "YSK404",
		"now/now.json":                           "Asia/Jakarta",
	} {
		if !strings.Contains(reps[label].Text, want) {
			t.Errorf("%s is missing %q:\n%s", label, want, reps[label].Text)
		}
	}
}

func TestRenderLayerDrawsChartsInTheSVGNamespace(t *testing.T) {
	reps := renderViewsInBrowser(t, fixtureCases(t))
	if r := reps["period_report/period_report.json"]; r.Circles != 4 || r.Foreign != 0 {
		t.Errorf("donut drew %d circles (%d outside SVG), want 4 (track + 3 slices)", r.Circles, r.Foreign)
	}
	if r := reps["cashflow_report/cashflow_report.json"]; r.Rects != 6 || r.Foreign != 0 {
		t.Errorf("cashflow drew %d bars (%d outside SVG), want 6", r.Rects, r.Foreign)
	}
}

// TestRenderLayerChartAttributesCannotBreakOut is the browser half of TestChartAttributesCannotBreakOut: a slice colour reaches a stroke attribute and nothing else.
func TestRenderLayerChartAttributesCannotBreakOut(t *testing.T) {
	const hostile = `#000" onmouseover="alert(1)`
	reps := renderViewsInBrowser(t, []viewCase{{
		Label: "donut",
		Tool:  "period_report",
		Data:  json.RawMessage(`{"spendByCategory":[{"category":{"name":"x","color":` + jsonQuote(hostile) + `},"share":1}]}`),
	}})
	r := reps["donut"]
	if r.Handlers != 0 {
		t.Errorf("a slice colour minted %d event-handler attributes:\n%s", r.Handlers, r.Inner)
	}
	for _, s := range r.Strokes {
		if strings.Contains(s, "onmouseover") {
			t.Errorf("stroke = %q carries the attacker's payload", s)
		}
	}
	for _, s := range r.Styles {
		if strings.Contains(s, "onmouseover") || strings.Count(s, ";") > 1 {
			t.Errorf("style = %q carries the attacker's payload", s)
		}
	}
}

func TestRenderLayerKeepsSwatchStylesToTheirDeclarations(t *testing.T) {
	reps := renderViewsInBrowser(t, []viewCase{
		{Label: "categories", Tool: "list_categories", Data: json.RawMessage(`{"categories":[{"id":"c","name":"x","color":"#000;position:fixed;inset:0;opacity:0"}]}`)},
		{Label: "report", Tool: "period_report", Data: json.RawMessage(`{"spendByCategory":[{"category":{"name":"x","color":"#000;position:fixed"},"share":"1;position:fixed"}]}`)},
	})
	for label, r := range reps {
		if r.Positions != 0 {
			t.Errorf("%s: a colour token repositioned %d elements", label, r.Positions)
		}
		for _, s := range r.Styles {
			if strings.Contains(s, "position") {
				t.Errorf("%s: style = %q carries extra declarations", label, s)
			}
		}
	}
}

func TestRenderLayerEscapesYasakuToolResults(t *testing.T) {
	const evil = `<img src=x onerror=alert(1)><script>alert(2)</` + `script>`
	q := jsonQuote(evil)
	reps := renderViewsInBrowser(t, []viewCase{
		{Label: "list_wallets", Tool: "list_wallets", Data: json.RawMessage(`{"wallets":[{"name":` + q + `}]}`)},
		{Label: "list_recent_tx", Tool: "list_recent_tx", Data: json.RawMessage(`{"transactions":[{"id":"x","kind":"expense","note":` + q + `}]}`)},
		{Label: "get_wallet", Tool: "get_wallet", Data: json.RawMessage(`{"wallet":{"id":"w","name":` + q + `}}`)},
		{Label: "wallet_totals", Tool: "wallet_totals", Data: json.RawMessage(`{"wallets":[{"wallet":{"id":"w","name":` + q + `}}]}`)},
		{Label: "period_report", Tool: "period_report", Data: json.RawMessage(`{"period":{"name":` + q + `}}`)},
		{Label: "cashflow_report", Tool: "cashflow_report", Data: json.RawMessage(`{"points":[{"period":{"name":` + q + `}}]}`)},
		{Label: "list_categories", Tool: "list_categories", Data: json.RawMessage(`{"categories":[{"id":"c","name":` + q + `}]}`)},
		{Label: "list_periods", Tool: "list_periods", Data: json.RawMessage(`{"periods":[{"id":"p","name":` + q + `}]}`)},
		{Label: "list_projects", Tool: "list_projects", Data: json.RawMessage(`{"projects":[{"org":"o","orgName":` + q + `}]}`)},
		{Label: "current_period", Tool: "current_period", Data: json.RawMessage(`{"period":{"id":"p","name":` + q + `}}`)},
		{Label: "now", Tool: "now", Data: json.RawMessage(`{"timezone":` + q + `,"currentPeriod":{"id":"p","name":"x"}}`)},
		{Label: "create_wallet", Tool: "create_wallet", Data: json.RawMessage(`{"needs":{"needs":[{"field":"kind","reason":` + q + `,"candidates":[` + q + `]}]}}`)},
		{Label: "record_batch", Tool: "record_batch", Data: json.RawMessage(`{"preview":[{"error":` + q + `}]}`)},
	})
	for label, r := range reps {
		if r.Images != 0 || r.Scripts != 0 || r.Handlers != 0 {
			t.Errorf("%s: a tool result was parsed as markup: %d img, %d script, %d handlers:\n%s", label, r.Images, r.Scripts, r.Handlers, r.Inner)
		}
		if !strings.Contains(r.Text, "<img src=x onerror=alert(1)>") {
			t.Errorf("%s: the hostile value is not visible as text:\n%s", label, r.Text)
		}
	}
}

func TestRenderLayerNeedsFormSubmitsItsOwnFields(t *testing.T) {
	reps := renderViewsInBrowser(t, []viewCase{
		{Label: "kind", Tool: "create_wallet", Data: json.RawMessage(fixtureJSON(t, "create_wallet_needs.json"))},
		{Label: "org", Tool: "create_wallet", Data: json.RawMessage(fixtureJSON(t, "mutation_org_needs.json"))},
		{Label: "bare", Tool: "record_expense", Data: json.RawMessage(fixtureJSON(t, "mutation_needs_no_candidates.json"))},
	})
	for label, want := range map[string]string{"kind": "kind", "org": "target.org"} {
		r := reps[label]
		if len(r.Fields) != 1 || r.Fields[0] != want {
			t.Errorf("%s: form fields = %v, want [%s]", label, r.Fields, want)
		}
		if len(r.Clicked) != 1 || r.Clicked[0].ID != "edit" || !r.Clicked[0].Form || len(r.Clicked[0].Fields) != 1 || r.Clicked[0].Fields[0] != want {
			t.Errorf("%s: Continue dispatched %+v, want one edit action carrying its form", label, r.Clicked)
		}
		if len(r.Submitted) != 1 || r.Submitted[0].ID != "edit" || !r.SubmitPrevented {
			t.Errorf("%s: Enter in the form dispatched %+v (prevented=%v); an unhandled submit navigates the frame", label, r.Submitted, r.SubmitPrevented)
		}
	}
	if !strings.Contains(reps["bare"].Inner, "<input") {
		t.Errorf("a need with no candidates must render a free-text input:\n%s", reps["bare"].Inner)
	}
}

func TestRenderLayerPreviewOffersCommitAndEdit(t *testing.T) {
	reps := renderViewsInBrowser(t, []viewCase{
		{Label: "expense", Tool: "record_expense", Data: json.RawMessage(fixtureJSON(t, "record_expense_preview.json"))},
		{Label: "result", Tool: "create_wallet", Data: json.RawMessage(fixtureJSON(t, "create_wallet_result.json"))},
	})
	if got := reps["expense"].Buttons; len(got) != 2 || got[0] != "Save" || got[1] != "Edit" {
		t.Errorf("preview buttons = %v, want [Save Edit]", got)
	}
	if got := reps["expense"].Clicked; len(got) != 1 || got[0].ID != "commit" {
		t.Errorf("the first preview button dispatched %+v, want commit", got)
	}
	if got := reps["result"].Buttons; len(got) != 0 {
		t.Errorf("a result offers nothing to click, got %v", got)
	}
}

func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
