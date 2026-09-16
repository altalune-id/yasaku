package templates

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/web"

	"github.com/a-h/templ"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestChartAssets(t *testing.T) {
	tests := []struct {
		name string
		mode web.UIMode
		want string
	}{
		{"cdn", web.UIModeCDN, "https://cdn.jsdelivr.net/npm/echarts@" + echartsVersion + "/dist/echarts.min.js"},
		{"vendored", web.UIModeVendored, "/static/echarts.min.js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, chartAssets(web.LayoutData{UIMode: tt.mode}))
			if !strings.Contains(got, tt.want) {
				t.Fatalf("chartAssets() = %q, want it to contain %q", got, tt.want)
			}
			if !strings.Contains(got, "/static/charts.js") {
				t.Fatalf("chartAssets() must always load charts.js, got %q", got)
			}
			if n := strings.Count(got, "<script defer"); n != 2 {
				t.Fatalf("chartAssets() emitted %d deferred scripts, want 2: %q", n, got)
			}
		})
	}
}

func TestChart(t *testing.T) {
	payload := ChartJSON(map[string]any{"items": []map[string]any{{"name": "Food", "value": 4000000}}})
	got := render(t, Chart("spend-by-category", "donut", payload, "h-72"))

	for _, want := range []string{
		`id="spend-by-category"`,
		`data-chart="donut"`,
		`class="min-h-[16rem] w-full h-72"`,
		`<script type="application/json" data-chart-for="spend-by-category">`,
		`"name":"Food"`,
		`"value":4000000`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Chart() = %q, want it to contain %q", got, want)
		}
	}
}

func TestChart_PayloadCannotBreakOutOfTheScriptElement(t *testing.T) {
	got := render(t, Chart("c1", "donut", `{"n":"</script><script>alert(1)</script>"}`, ""))
	if strings.Count(got, "</script>") != 1 {
		t.Fatalf("Chart() = %q, want exactly one closing script tag", got)
	}
	if strings.Contains(got, "alert(1)</script>") {
		t.Fatalf("Chart() = %q, payload escaped its own script element", got)
	}
}

func TestChartJSON(t *testing.T) {
	if got := ChartJSON(map[string]any{"a": 1}); got != `{"a":1}` {
		t.Fatalf("ChartJSON() = %q, want %q", got, `{"a":1}`)
	}
	if got := ChartJSON(make(chan int)); got != "null" {
		t.Fatalf("ChartJSON() on an unencodable value = %q, want %q", got, "null")
	}
}

func TestChartJSON_EscapesHTML(t *testing.T) {
	const raw = "</script><b>&"
	got := ChartJSON(map[string]string{"n": raw})
	if strings.ContainsAny(got, "<>&") {
		t.Fatalf("ChartJSON() = %q, must not emit a raw <, > or & — the payload sits inside a script element", got)
	}
	var back map[string]string
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("ChartJSON() produced invalid JSON %q: %v", got, err)
	}
	if back["n"] != raw {
		t.Fatalf("round-trip = %q, want %q", back["n"], raw)
	}
}
