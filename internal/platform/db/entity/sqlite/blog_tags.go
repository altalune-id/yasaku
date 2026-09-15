package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// BlogTags is the jet binding for the blog_tags table.
type BlogTags struct {
	sqlite.Table

	ID        sqlite.ColumnString
	OrgID     sqlite.ColumnString
	ProjectID sqlite.ColumnString
	Name      sqlite.ColumnString
	Slug      sqlite.ColumnString
	CreatedAt sqlite.ColumnString
	UpdatedAt sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewBlogTags builds the blog_tags binding.
func NewBlogTags(tablePrefix string) *BlogTags {
	var (
		id        = sqlite.StringColumn("id")
		orgID     = sqlite.StringColumn("org_id")
		projectID = sqlite.StringColumn("project_id")
		name      = sqlite.StringColumn("name")
		slug      = sqlite.StringColumn("slug")
		createdAt = sqlite.StringColumn("created_at")
		updatedAt = sqlite.StringColumn("updated_at")
		all       = sqlite.ColumnList{id, orgID, projectID, name, slug, createdAt, updatedAt}
	)
	return &BlogTags{
		Table:      sqlite.NewTable("", tablePrefix+"blog_tags", "blog_tags", all...),
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
