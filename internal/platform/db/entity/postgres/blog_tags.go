package postgres

import "github.com/go-jet/jet/v2/postgres"

// BlogTags is the jet binding for the blog_tags table.
type BlogTags struct {
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

// NewBlogTags builds the blog_tags binding.
func NewBlogTags(schema, tablePrefix string) *BlogTags {
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
	return &BlogTags{
		Table:      postgres.NewTable(schema, tablePrefix+"blog_tags", "blog_tags", all...),
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
