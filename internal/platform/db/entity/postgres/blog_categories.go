package postgres

import "github.com/go-jet/jet/v2/postgres"

// BlogCategories is the jet binding for the blog_categories table.
type BlogCategories struct {
	postgres.Table

	ID        postgres.ColumnString
	OrgID     postgres.ColumnString
	ProjectID postgres.ColumnString
	Name      postgres.ColumnString
	Slug      postgres.ColumnString
	CreatedAt postgres.ColumnTimestampz
	UpdatedAt postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewBlogCategories builds the blog_categories binding.
func NewBlogCategories(schema, tablePrefix string) *BlogCategories {
	if schema == "" {
		schema = "public"
	}
	var (
		id        = postgres.StringColumn("id")
		orgID     = postgres.StringColumn("org_id")
		projectID = postgres.StringColumn("project_id")
		name      = postgres.StringColumn("name")
		slug      = postgres.StringColumn("slug")
		createdAt = postgres.TimestampzColumn("created_at")
		updatedAt = postgres.TimestampzColumn("updated_at")
		all       = postgres.ColumnList{id, orgID, projectID, name, slug, createdAt, updatedAt}
	)
	return &BlogCategories{
		Table:      postgres.NewTable(schema, tablePrefix+"blog_categories", "blog_categories", all...),
		ID:         id,
		OrgID:      orgID,
		ProjectID:  projectID,
		Name:       name,
		Slug:       slug,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		AllColumns: all,
	}
}
