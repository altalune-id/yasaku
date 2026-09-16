package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// PeriodClosings is the jet binding for the period_closings table.
type PeriodClosings struct {
	sqlite.Table

	ID        sqlite.ColumnString
	OrgID     sqlite.ColumnString
	ProjectID sqlite.ColumnString
	PeriodID  sqlite.ColumnString
	ClosedAt  sqlite.ColumnString
	ClosedBy  sqlite.ColumnString
	Snapshot  sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewPeriodClosings builds the period_closings binding.
func NewPeriodClosings(tablePrefix string) *PeriodClosings {
	var (
		id        = sqlite.StringColumn("id")
		orgID     = sqlite.StringColumn("org_id")
		projectID = sqlite.StringColumn("project_id")
		periodID  = sqlite.StringColumn("period_id")
		closedAt  = sqlite.StringColumn("closed_at")
		closedBy  = sqlite.StringColumn("closed_by")
		snapshot  = sqlite.StringColumn("snapshot")
		all       = sqlite.ColumnList{id, orgID, projectID, periodID, closedAt, closedBy, snapshot}
	)
	return &PeriodClosings{
		Table:      sqlite.NewTable("", tablePrefix+"period_closings", "period_closings", all...),
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
