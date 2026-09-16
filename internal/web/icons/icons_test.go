package icons

import "testing"

func TestHas_YasakuIconsAreVendored(t *testing.T) {
	names := []string{
		"wallet", "arrow-left-right", "calendar-range", "chart-pie", "settings",
		"receipt", "banknote", "circle-plus", "lock", "lock-open", "rotate-ccw",
		"layout-dashboard", "folder-tree",
	}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			if !Has(n) {
				t.Fatalf("icon %q must be vendored — the yasaku surface references it", n)
			}
			inner, ok := InnerFor(n)
			if !ok || inner == "" {
				t.Fatalf("icon %q has an empty SVG body", n)
			}
		})
	}
}
