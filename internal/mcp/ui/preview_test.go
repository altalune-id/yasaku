package ui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const previewDriver = `
for (const c of __previewCases) {
  const section = document.createElement("section");
  section.className = "pv";
  const h = document.createElement("h2");
  h.textContent = c.label;
  section.appendChild(h);
  const app = document.createElement("yasaku-app");
  section.appendChild(app);
  document.body.appendChild(section);
  app.view = renderTool(c.tool, c.data);
  app.status = "view";
}
`

// TestDumpPreview writes every view against its fixture into one page that renders on load, so layout can be checked without a host. Opt in with YASAKU_UI_PREVIEW=<path>.
func TestDumpPreview(t *testing.T) {
	path := os.Getenv("YASAKU_UI_PREVIEW")
	if path == "" {
		t.Skip("set YASAKU_UI_PREVIEW=path to write the rendered preview")
	}
	type previewCase struct {
		Label string          `json:"label"`
		Tool  string          `json:"tool"`
		Data  json.RawMessage `json:"data"`
	}
	cases := make([]previewCase, 0, len(viewFixtures))
	for _, f := range viewFixtures {
		cases = append(cases, previewCase{Label: f.tool + " (" + f.fixture + ")", Tool: f.tool, Data: json.RawMessage(fixtureJSON(t, f.fixture))})
	}
	data, err := json.Marshal(cases)
	if err != nil {
		t.Fatalf("marshal cases: %v", err)
	}

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>yasaku MCP views</title><style>\n")
	b.WriteString(mustRead(t, "app.css"))
	b.WriteString("\nbody{padding:24px;background:light-dark(#fff,#111)}")
	b.WriteString("\n.pv{max-width:520px;margin:0 0 28px;border:1px dashed #888;border-radius:14px}")
	b.WriteString("\n.pv h2{font:600 12px ui-monospace,monospace;margin:0;padding:8px 12px;border-bottom:1px dashed #888;opacity:.7}")
	b.WriteString("\n</style></head><body>\n<script type=\"module\">")
	b.WriteString(exposeExports(mustRead(t, litPart), "__lit"))
	b.WriteString("</script>\n<script type=\"module\">\n")
	for _, p := range scriptParts {
		if domParts[p] {
			continue
		}
		b.WriteString(mustRead(t, p))
		b.WriteString("\n")
	}
	b.WriteString("const __previewCases = ")
	b.WriteString(strings.ReplaceAll(string(data), "</", `<\/`))
	b.WriteString(";\n")
	b.WriteString(previewDriver)
	b.WriteString("</script></body></html>\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %s", path)
}

func TestDumpBundle(t *testing.T) {
	path := os.Getenv("YASAKU_UI_DUMP")
	if path == "" {
		t.Skip("set YASAKU_UI_DUMP=path to write the assembled bundle")
	}
	if err := os.WriteFile(path, []byte(Document()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %s", path)
}
