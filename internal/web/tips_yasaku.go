package web

//i18n:use tips.*

// Tips returns the registry of every tip the server may render or mark as seen.
func Tips() []Tip {
	return []Tip{
		{ID: "add-first", Flow: "start", TextKey: "tips.add_first", Target: "add"},
		{ID: "row-actions", Flow: "start", TextKey: "tips.row_actions"},
		{ID: "recent-fill", Flow: "start", TextKey: "tips.recent_fill"},
	}
}
