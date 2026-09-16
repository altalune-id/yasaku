package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// Categories is the jet binding for the categories table.
type Categories struct {
	sqlite.Table

	ID         sqlite.ColumnString
	OrgID      sqlite.ColumnString
	ProjectID  sqlite.ColumnString
	Name       sqlite.ColumnString
	Kind       sqlite.ColumnString
	Icon       sqlite.ColumnString
	Color      sqlite.ColumnString
	SortOrder  sqlite.ColumnInteger
	ArchivedAt sqlite.ColumnString
	CreatedAt  sqlite.ColumnString
	UpdatedAt  sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewCategories builds the categories binding.
func NewCategories(tablePrefix string) *Categories {
	var (
		id         = sqlite.StringColumn("id")
		orgID      = sqlite.StringColumn("org_id")
		projectID  = sqlite.StringColumn("project_id")
		name       = sqlite.StringColumn("name")
		kind       = sqlite.StringColumn("kind")
		icon       = sqlite.StringColumn("icon")
		color      = sqlite.StringColumn("color")
		sortOrder  = sqlite.IntegerColumn("sort_order")
		archivedAt = sqlite.StringColumn("archived_at")
		createdAt  = sqlite.StringColumn("created_at")
		updatedAt  = sqlite.StringColumn("updated_at")
		all        = sqlite.ColumnList{id, orgID, projectID, name, kind, icon, color, sortOrder, archivedAt, createdAt, updatedAt}
	)
	return &Categories{
		Table:      sqlite.NewTable("", tablePrefix+"categories", "categories", all...),
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
