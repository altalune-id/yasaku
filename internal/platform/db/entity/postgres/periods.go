package postgres

import "github.com/go-jet/jet/v2/postgres"

// Periods is the jet binding for the periods table.
type Periods struct {
	postgres.Table

	ID        postgres.ColumnString
	OrgID     postgres.ColumnString
	ProjectID postgres.ColumnString
	Name      postgres.ColumnString
	StartDate postgres.ColumnDate
	EndDate   postgres.ColumnDate
	Status    postgres.ColumnString
	ClosedAt  postgres.ColumnTimestampz
	Snapshot  postgres.ColumnString
	CreatedAt postgres.ColumnTimestampz
	UpdatedAt postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewPeriods builds the periods binding.
func NewPeriods(schema, tablePrefix string) *Periods {
	if schema == "" {
		schema = "public"
	}
	var (
		id        = postgres.StringColumn("id")
		orgID     = postgres.StringColumn("org_id")
		projectID = postgres.StringColumn("project_id")
		name      = postgres.StringColumn("name")
		startDate = postgres.DateColumn("start_date")
		endDate   = postgres.DateColumn("end_date")
		status    = postgres.StringColumn("status")
		closedAt  = postgres.TimestampzColumn("closed_at")
		snapshot  = postgres.StringColumn("snapshot")
		createdAt = postgres.TimestampzColumn("created_at")
		updatedAt = postgres.TimestampzColumn("updated_at")
		all       = postgres.ColumnList{id, orgID, projectID, name, startDate, endDate, status, closedAt, snapshot, createdAt, updatedAt}
	)
	return &Periods{
		Table:      postgres.NewTable(schema, tablePrefix+"periods", "periods", all...),
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
