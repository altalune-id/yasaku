package templates

import (
	"strconv"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/web"
)

//i18n:use date.*

// LedgerDateText is a ledger date as plain text in the page locale, for option elements and translation arguments.
func LedgerDateText(d web.LayoutData, day civil.Date) string {
	return dateText(d, day, "medium", "short")
}

func dateText(d web.LayoutData, day civil.Date, pattern, month string) string {
	patternKey := "date.pattern." + pattern
	monthKey := "date.month_" + month + "." + strconv.Itoa(int(day.Month))
	return d.Tr(patternKey, "Day", day.Day, "Month", d.Tr(monthKey), "Year", day.Year)
}

// PeriodShortLabelText is a period's chart axis label: the short month for a whole-month period, else its start day and month.
func PeriodShortLabelText(d web.LayoutData, l PeriodLabel) string {
	if l.IsMonth() {
		monthKey := "date.month_short." + strconv.Itoa(int(l.Start.Month))
		return d.Tr(monthKey)
	}
	return dateText(d, l.Start, "compact", "short")
}
