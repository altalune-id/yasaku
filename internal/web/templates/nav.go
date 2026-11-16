package templates

import "altalune.id/yasaku/internal/web"

type navItem struct {
	Key      string
	Path     string
	Icon     string
	LabelKey string
	InDock   bool
	More     bool
	Show     func(web.LayoutData) bool
}

func navVisible(d web.LayoutData, more bool) []navItem {
	var out []navItem
	for _, it := range projectNav() {
		if it.More == more && (it.Show == nil || it.Show(d)) {
			out = append(out, it)
		}
	}
	return out
}

func dockItems() []navItem {
	var out []navItem
	for _, it := range projectNav() {
		if it.InDock {
			out = append(out, it)
		}
	}
	return out
}
