package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"altalune.id/yasaku/internal/web"
)

func TestDateField_MaxCapsTheNativeInputAndTheCalendar(t *testing.T) {
	out := render(t, DateField(web.LayoutData{}, DateFieldProps{ID: "d", Name: "date", Value: "2026-11-17", Today: "2026-11-17", Yesterday: "2026-11-16", Max: "2026-11-17"}))
	requireContains(t, out, `data-max="2026-11-17"`)
	if n := strings.Count(out, `max="2026-11-17"`); n != 3 {
		t.Errorf("want data-max, the native input max and the calendar max, got %d", n)
	}
	if strings.Contains(render(t, DateField(web.LayoutData{}, DateFieldProps{ID: "d", Name: "date"})), "max=") {
		t.Error("a field without Max must not cap anything")
	}
}

const dateMaxProbe = `
window.addEventListener("load", function () {
  customElements.whenDefined("calendar-date").then(function () { setTimeout(probe, 400); });
});
function pickable(iso) {
  var cal = document.querySelector("calendar-date");
  cal.setAttribute("focused-date", iso);
  return new Promise(function (done) {
    setTimeout(function () {
      var month = cal.querySelector("calendar-month").shadowRoot;
      var days = Array.prototype.filter.call(month.querySelectorAll("button"), function (b) {
        var p = b.getAttribute("part") || "";
        return p.indexOf("day") >= 0 && p.indexOf("outside") < 0;
      });
      var b = days[Number(iso.slice(8)) - 1];
      done(b ? !b.disabled : null);
    }, 200);
  });
}
function probe() {
  var c = window.DATE_CASES, out = [], i = 0;
  (function next() {
    if (i === c.length) {
      out.push("input max " + document.querySelector("[data-date-input]").max);
      document.getElementById("out").textContent = btoa(unescape(encodeURIComponent(JSON.stringify(out))));
      return;
    }
    var k = c[i++];
    pickable(k.day).then(function (ok) {
      if (ok !== k.want) out.push(k.day + " pickable=" + ok + ", want " + k.want);
      next();
    });
  })();
}
`

func runDateMax(t *testing.T, maxDate string, cases string) []string {
	t.Helper()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), "internal/web/static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	field := render(t, DateField(web.LayoutData{}, DateFieldProps{ID: "d", Name: "date", Value: maxDate, Today: maxDate, Yesterday: maxDate, Max: maxDate}))
	body := field + `<script>window.DATE_CASES = ` + cases + `;</script><script type="module">` + read("cally.js") + `</script><script>` + read("datepicker.js") + `</script>`
	return runLayoutProbe(t, 1280, 800, body, dateMaxProbe)
}

func iso(d time.Time) string { return d.Format("2006-01-02") }

// TestDatePicker_BlocksFutureDays checks the browser: tomorrow is disabled, today is not, and a project already on a later day keeps that day pickable.
func TestDatePicker_BlocksFutureDays(t *testing.T) {
	today := time.Now()
	t.Run("max is the browser's today", func(t *testing.T) {
		cases := fmt.Sprintf(`[{"day":%q,"want":true},{"day":%q,"want":false}]`, iso(today), iso(today.AddDate(0, 0, 1)))
		got := runDateMax(t, iso(today), cases)
		if len(got) != 1 || got[0] != "input max "+iso(today) {
			t.Fatal(strings.Join(got, "\n"))
		}
	})
	t.Run("project ahead of the browser", func(t *testing.T) {
		ahead := today.AddDate(0, 0, 2)
		cases := fmt.Sprintf(`[{"day":%q,"want":true},{"day":%q,"want":false}]`, iso(ahead), iso(ahead.AddDate(0, 0, 1)))
		got := runDateMax(t, iso(ahead), cases)
		if len(got) != 1 || got[0] != "input max "+iso(ahead) {
			t.Fatal(strings.Join(got, "\n"))
		}
	})
	t.Run("project behind the browser", func(t *testing.T) {
		behind := today.AddDate(0, 0, -1)
		cases := fmt.Sprintf(`[{"day":%q,"want":true},{"day":%q,"want":false}]`, iso(today), iso(today.AddDate(0, 0, 1)))
		got := runDateMax(t, iso(behind), cases)
		if len(got) != 1 || got[0] != "input max "+iso(today) {
			t.Fatal(strings.Join(got, "\n"))
		}
	})
}
