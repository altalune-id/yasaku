package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// BlogCategories is the jet binding for the blog_categories table.
type BlogCategories struct {
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

// NewBlogCategories builds the blog_categories binding.
func NewBlogCategories(tablePrefix string) *BlogCategories {
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
	return &BlogCategories{
		Table:      sqlite.NewTable("", tablePrefix+"blog_categories", "blog_categories", all...),
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
