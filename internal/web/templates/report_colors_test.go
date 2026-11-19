package templates

import (
	"strings"
	"testing"
)

func TestReportSlotColor_EveryPartNamesTheSameColor(t *testing.T) {
	seen := map[string]bool{}
	for slot := 0; slot <= 5; slot++ {
		c := ReportSlotColor(slot)
		name := strings.TrimPrefix(c.Var, "--color-")
		if c.Dot != "bg-"+name || c.Bar != "progress-"+name || reportSlotDot(slot) != c.Dot || reportSlotBar(slot) != c.Bar {
			t.Errorf("slot %d drifts: %+v, dot %q, bar %q", slot, c, reportSlotDot(slot), reportSlotBar(slot))
		}
		if seen[name] {
			t.Errorf("slot %d reuses %s", slot, name)
		}
		seen[name] = true
	}
	if ReportSlotColor(9) != ReportSlotColor(0) || ReportSlotColor(-1) != ReportSlotColor(0) {
		t.Error("an unknown slot is not Other")
	}
}
