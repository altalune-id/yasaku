package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const railTipProbe = `
window.addEventListener("load", function () {
  setTimeout(function () {
    var out = [];
    var target = document.querySelector('[data-tour="add"]').getBoundingClientRect();
    var tip = document.querySelector('[data-tip-for="add"]');
    var links = document.getElementById("links").getBoundingClientRect();
    var page = document.getElementById("page").getBoundingClientRect();
    var card = tip.getBoundingClientRect();
    if (tip.hidden) out.push("tip hidden");
    if (card.top < target.bottom) out.push("card top " + Math.round(card.top) + " is above the rail target bottom " + Math.round(target.bottom));
    if (card.bottom > links.top + 1) out.push("card bottom " + Math.round(card.bottom) + " covers the rail links starting at " + Math.round(links.top));
    if (card.right > page.left) out.push("card right " + Math.round(card.right) + " spills into the page starting at " + Math.round(page.left));
    document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
  }, 300);
});
`

// TestTip_ARailTargetGetsTheCardInTheRailFlow keeps the add tip card off both the rail links and the page on desktop.
func TestTip_ARailTargetGetsTheCardInTheRailFlow(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static/tips.js"))
	if err != nil {
		t.Fatal(err)
	}
	body := `<div class="drawer lg:drawer-open"><input id="d" type="checkbox" class="drawer-toggle"><div class="drawer-content"><div id="page">Page</div>` +
		render(t, Tip(tipData(), addTipID)) +
		`</div><div class="drawer-side"><aside class="flex w-64 flex-col gap-4 p-4"><button type="button" data-tour="add" class="btn w-full">Add</button><ul id="links"><li>Home</li></ul></aside></div></div>` +
		`<script>` + string(script) + `</script>`
	if got := runLayoutProbe(t, 1280, 800, body, railTipProbe); len(got) > 0 {
		t.Fatal(strings.Join(got, "\n"))
	}
}
