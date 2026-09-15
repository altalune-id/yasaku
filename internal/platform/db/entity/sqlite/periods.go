package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// Periods is the jet binding for the periods table.
type Periods struct {
	sqlite.Table

	ID        sqlite.ColumnString
	OrgID     sqlite.ColumnString
	ProjectID sqlite.ColumnString
	Name      sqlite.ColumnString
	StartDate sqlite.ColumnString
	EndDate   sqlite.ColumnString
	Status    sqlite.ColumnString
	ClosedAt  sqlite.ColumnString
	Snapshot  sqlite.ColumnString
	CreatedAt sqlite.ColumnString
	UpdatedAt sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewPeriods builds the periods binding.
func NewPeriods(tablePrefix string) *Periods {
	var (
		id        = sqlite.StringColumn("id")
		orgID     = sqlite.StringColumn("org_id")
		projectID = sqlite.StringColumn("project_id")
		name      = sqlite.StringColumn("name")
		startDate = sqlite.StringColumn("start_date")
		endDate   = sqlite.StringColumn("end_date")
		status    = sqlite.StringColumn("status")
		closedAt  = sqlite.StringColumn("closed_at")
		snapshot  = sqlite.StringColumn("snapshot")
		createdAt = sqlite.StringColumn("created_at")
		updatedAt = sqlite.StringColumn("updated_at")
		all       = sqlite.ColumnList{id, orgID, projectID, name, startDate, endDate, status, closedAt, snapshot, createdAt, updatedAt}
	)
	return &Periods{
		Table:      sqlite.NewTable("", tablePrefix+"periods", "periods", all...),
		ID:         id,
		OrgID:      orgID,
		ProjectID:  projectID,
		Name:       name,
		StartDate:  startDate,
		EndDate:    endDate,
		Status:     status,
		ClosedAt:   closedAt,
		Snapshot:   snapshot,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		AllColumns: all,
	}
}
