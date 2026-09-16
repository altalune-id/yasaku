package postgres

import "github.com/go-jet/jet/v2/postgres"

// PeriodClosings is the jet binding for the period_closings table.
type PeriodClosings struct {
	postgres.Table

	ID        postgres.ColumnString
	OrgID     postgres.ColumnString
	ProjectID postgres.ColumnString
	PeriodID  postgres.ColumnString
	ClosedAt  postgres.ColumnTimestampz
	ClosedBy  postgres.ColumnString
	Snapshot  postgres.ColumnString

	AllColumns postgres.ColumnList
}

// NewPeriodClosings builds the period_closings binding.
func NewPeriodClosings(schema, tablePrefix string) *PeriodClosings {
	if schema == "" {
		schema = "public"
	}
	var (
		id        = postgres.StringColumn("id")
		orgID     = postgres.StringColumn("org_id")
		projectID = postgres.StringColumn("project_id")
		periodID  = postgres.StringColumn("period_id")
		closedAt  = postgres.TimestampzColumn("closed_at")
		closedBy  = postgres.StringColumn("closed_by")
		snapshot  = postgres.StringColumn("snapshot")
		all       = postgres.ColumnList{id, orgID, projectID, periodID, closedAt, closedBy, snapshot}
	)
	return &PeriodClosings{
		Table:      postgres.NewTable(schema, tablePrefix+"period_closings", "period_closings", all...),
		ID:         id,
		OrgID:      orgID,
		ProjectID:  projectID,
		PeriodID:   periodID,
		ClosedAt:   closedAt,
		ClosedBy:   closedBy,
		Snapshot:   snapshot,
		AllColumns: all,
	}
}
