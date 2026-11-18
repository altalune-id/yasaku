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
