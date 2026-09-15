package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// LedgerSettings is the jet binding for the ledger_settings table.
type LedgerSettings struct {
	sqlite.Table

	ProjectID      sqlite.ColumnString
	OrgID          sqlite.ColumnString
	Timezone       sqlite.ColumnString
	Currency       sqlite.ColumnString
	PeriodStartDay sqlite.ColumnInteger
	UpdatedAt      sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewLedgerSettings builds the ledger_settings binding.
func NewLedgerSettings(tablePrefix string) *LedgerSettings {
	var (
		projectID      = sqlite.StringColumn("project_id")
		orgID          = sqlite.StringColumn("org_id")
		timezone       = sqlite.StringColumn("timezone")
		currency       = sqlite.StringColumn("currency")
		periodStartDay = sqlite.IntegerColumn("period_start_day")
		updatedAt      = sqlite.StringColumn("updated_at")
		all            = sqlite.ColumnList{projectID, orgID, timezone, currency, periodStartDay, updatedAt}
	)
	return &LedgerSettings{
		Table:          sqlite.NewTable("", tablePrefix+"ledger_settings", "ledger_settings", all...),
		ProjectID:      projectID,
		OrgID:          orgID,
		Timezone:       timezone,
		Currency:       currency,
		PeriodStartDay: periodStartDay,
		UpdatedAt:      updatedAt,
		AllColumns:     all,
	}
}
