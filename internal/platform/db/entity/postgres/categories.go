package postgres

import "github.com/go-jet/jet/v2/postgres"

// Categories is the jet binding for the categories table.
type Categories struct {
	postgres.Table

	ID         postgres.ColumnString
	OrgID      postgres.ColumnString
	ProjectID  postgres.ColumnString
	Name       postgres.ColumnString
	Kind       postgres.ColumnString
	Icon       postgres.ColumnString
	Color      postgres.ColumnString
	SortOrder  postgres.ColumnInteger
	ArchivedAt postgres.ColumnTimestampz
	CreatedAt  postgres.ColumnTimestampz
	UpdatedAt  postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewCategories builds the categories binding.
func NewCategories(schema, tablePrefix string) *Categories {
	if schema == "" {
		schema = "public"
	}
	var (
		id         = postgres.StringColumn("id")
		orgID      = postgres.StringColumn("org_id")
		projectID  = postgres.StringColumn("project_id")
		name       = postgres.StringColumn("name")
		kind       = postgres.StringColumn("kind")
		icon       = postgres.StringColumn("icon")
		color      = postgres.StringColumn("color")
		sortOrder  = postgres.IntegerColumn("sort_order")
		archivedAt = postgres.TimestampzColumn("archived_at")
		createdAt  = postgres.TimestampzColumn("created_at")
		updatedAt  = postgres.TimestampzColumn("updated_at")
		all        = postgres.ColumnList{id, orgID, projectID, name, kind, icon, color, sortOrder, archivedAt, createdAt, updatedAt}
	)
	return &Categories{
		Table:      postgres.NewTable(schema, tablePrefix+"categories", "categories", all...),
		ID:         id,
		OrgID:      orgID,
		ProjectID:  projectID,
		Name:       name,
		Kind:       kind,
		Icon:       icon,
		Color:      color,
		SortOrder:  sortOrder,
		ArchivedAt: archivedAt,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		AllColumns: all,
	}
}
