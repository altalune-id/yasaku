package postgres

import "github.com/go-jet/jet/v2/postgres"

// LedgerSettings is the jet binding for the ledger_settings table.
type LedgerSettings struct {
	postgres.Table

	ProjectID      postgres.ColumnString
	OrgID          postgres.ColumnString
	Timezone       postgres.ColumnString
	Currency       postgres.ColumnString
	PeriodStartDay postgres.ColumnInteger
	UpdatedAt      postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewLedgerSettings builds the ledger_settings binding.
func NewLedgerSettings(schema, tablePrefix string) *LedgerSettings {
	if schema == "" {
		schema = "public"
	}
	var (
		projectID      = postgres.StringColumn("project_id")
		orgID          = postgres.StringColumn("org_id")
		timezone       = postgres.StringColumn("timezone")
		currency       = postgres.StringColumn("currency")
		periodStartDay = postgres.IntegerColumn("period_start_day")
		updatedAt      = postgres.TimestampzColumn("updated_at")
		all            = postgres.ColumnList{projectID, orgID, timezone, currency, periodStartDay, updatedAt}
	)
	return &LedgerSettings{
		Table:          postgres.NewTable(schema, tablePrefix+"ledger_settings", "ledger_settings", all...),
		ProjectID:      projectID,
		OrgID:          orgID,
		Timezone:       timezone,
		Currency:       currency,
		PeriodStartDay: periodStartDay,
		UpdatedAt:      updatedAt,
		AllColumns:     all,
	}
}
