package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const chartsProbeBody = `
<div id="d" data-chart="donut" style="width:256px;height:256px"></div>
<script type="application/json" data-chart-for="d">{"items":[{"name":"Food","value":300,"formatted":"Rp 300","color":"--color-primary"},{"name":"Bills","value":100,"formatted":"Rp 100","color":"--color-accent"},{"name":"Other","value":50,"formatted":"Rp 50","color":"--color-neutral"}]}</script>
<div id="b" data-chart="bars" style="width:390px;height:256px"></div>
<script type="application/json" data-chart-for="b">{"categories":["Oct","Nov"],"full":["October 2026","November 2026"],"income":[5,6],"expense":[3,4],"net":[2,-1],"formatted":{},"labels":{},"unit":{"divisor":1,"symbol":"Rp"}}</script>
<div id="s" data-chart="sankey" style="width:358px;height:224px"></div>
<script type="application/json" data-chart-for="s">{"nodes":[{"name":"Cash"},{"name":"Food","color":"--color-primary"}],"links":[{"source":"Cash","target":"Food","value":3,"tooltip":"Cash to Food: Rp 3"}]}</script>
<div id="c" data-chart="compare" style="width:358px;height:224px"></div>
<script type="application/json" data-chart-for="c">{"categories":["Food","Bills"],"current":[3,1],"previous":[2,2],"formatted":{},"labels":{"current":"Nov","previous":"Oct"},"unit":{"divisor":1,"symbol":"Rp"}}</script>
<dialog id="m"><div id="t" data-chart="trend" style="width:326px;height:224px"></div>
<script type="application/json" data-chart-for="t">{"labels":["Jun","Jul","Aug","Sep","Oct","N{b|x}"],"values":[0,1,2,0,3,4],"formatted":[],"current":5,"unit":{"divisor":1,"symbol":"Rp"}}</script></dialog>
`

const chartsProbe = `
window.addEventListener("load", function () {
  var out = [];
  function opt(id) { var c = echarts.getInstanceByDom(document.getElementById(id)); return c && c.getOption(); }
  var d = opt("d");
  if (!d) { out.push("donut not drawn"); } else {
    var cs = d.series[0].data.map(function (x) { return x.itemStyle.color; });
    if (!/^rgba\(/.test(cs[0]) || cs[0] === cs[1] || cs[1] === cs[2]) out.push("donut slot colors not resolved: " + cs.join(" | "));
    if (String(d.textStyle[0] ? d.textStyle[0].fontFamily : d.textStyle.fontFamily).indexOf("Figtree") < 0) out.push("donut font is not --font-sans");
  }
  var b = opt("b");
  if (!b || b.series.length !== 3 || b.series.some(function (s) { return s.type !== "bar"; })) out.push("cashflow is not three bar series");
  else b.series.forEach(function (s) { if (!s.itemStyle || !/^rgba\(/.test(s.itemStyle.color) || s.itemStyle.color !== s.data[0].itemStyle.color) out.push("cashflow series " + s.name + " legend color is not its theme color"); });
  var s = opt("s");
  if (s && (!s.series[0].label.textBorderColor || !s.series[0].label.textBorderWidth)) out.push("sankey labels have no halo");
  if (!s) out.push("sankey not drawn");
  else if (document.documentElement.dir === "rtl" && s.series[0].links[0].source !== "Food") out.push("rtl sankey keeps wallets on the left");
  else if (document.documentElement.dir !== "rtl" && s.series[0].links[0].source !== "Cash") out.push("ltr sankey swapped sides");
  var c = opt("c");
  if (!c) { out.push("compare not drawn"); } else {
    var lab = c.xAxis[0].axisLabel, rtl = document.documentElement.dir === "rtl";
    if (lab.alignMaxLabel !== (rtl ? "left" : "right") || lab.alignMinLabel !== (rtl ? "right" : "left")) out.push("compare end labels can spill past the chart: " + lab.alignMinLabel + "/" + lab.alignMaxLabel);
  }
  if (opt("t")) out.push("trend drawn inside a closed dialog");
  document.getElementById("m").showModal();
  setTimeout(function () {
    var tr = opt("t");
    if (!tr) { out.push("trend not drawn when its dialog opens"); } else {
      var ax = tr.xAxis[0], cur = ax.data[5];
      if (ax.axisLabel.rich || ax.axisLabel.formatter) out.push("trend labels go through rich text");
      if (!cur || cur.value !== "N{b|x}" || !cur.textStyle || cur.textStyle.fontWeight !== 600) out.push("current trend label is not a bold plain value: " + JSON.stringify(cur));
    }
    document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
  }, 200);
});
`

func TestChartsJS_DrawsEveryKindFromDaisyUIVariables(t *testing.T) {
	root := repoRoot(t)
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(root, "internal/web/static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	css, echarts, charts := read("app.css"), read("echarts.min.js"), read("charts.js")
	for _, dir := range []string{"ltr", "rtl"} {
		t.Run(dir, func(t *testing.T) {
			page := `<!doctype html><html lang="en-US" dir="` + dir + `" data-theme="light"><head><style>` + css + `</style></head><body>` +
				chartsProbeBody + `<pre id="out"></pre><script>` + echarts + `</script><script>` + charts + `</script><script>` + chartsProbe + `</script></body></html>`
			if got := runProbe(t, page, "--window-size=800,1200"); len(got) > 0 {
				t.Fatal(strings.Join(got, "\n"))
			}
		})
	}
}
