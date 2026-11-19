package templates

// ReportSlotColors is one category color: the CSS variable the charts read and the legend's dot and bar classes.
type ReportSlotColors struct {
	Var string
	Dot string
	Bar string
}

var reportSlots = [...]ReportSlotColors{ //nolint:gochecknoglobals // fixed color table
	{Var: "--color-secondary", Dot: "bg-secondary", Bar: "progress-secondary"},
	{Var: "--color-primary", Dot: "bg-primary", Bar: "progress-primary"},
	{Var: "--color-accent", Dot: "bg-accent", Bar: "progress-accent"},
	{Var: "--color-info", Dot: "bg-info", Bar: "progress-info"},
	{Var: "--color-success", Dot: "bg-success", Bar: "progress-success"},
	{Var: "--color-error", Dot: "bg-error", Bar: "progress-error"},
}

// ReportSlotColor is the color of a category slot; slot 0 and any unknown slot is Other.
func ReportSlotColor(slot int) ReportSlotColors {
	if slot < 1 || slot >= len(reportSlots) {
		return reportSlots[0]
	}
	return reportSlots[slot]
}
