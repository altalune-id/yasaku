package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// BlogPosts is the jet binding for the blog_posts table.
type BlogPosts struct {
	sqlite.Table

	ID               sqlite.ColumnString
	OrgID            sqlite.ColumnString
	ProjectID        sqlite.ColumnString
	CategoryID       sqlite.ColumnString
	Title            sqlite.ColumnString
	Slug             sqlite.ColumnString
	BodyMarkdown     sqlite.ColumnString
	Status           sqlite.ColumnString
	FirstPublishedAt sqlite.ColumnString
	CreatedAt        sqlite.ColumnString
	UpdatedAt        sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewBlogPosts builds the blog_posts binding.
func NewBlogPosts(tablePrefix string) *BlogPosts {
	var (
		id               = sqlite.StringColumn("id")
		orgID            = sqlite.StringColumn("org_id")
		projectID        = sqlite.StringColumn("project_id")
		categoryID       = sqlite.StringColumn("category_id")
		title            = sqlite.StringColumn("title")
		slug             = sqlite.StringColumn("slug")
		bodyMarkdown     = sqlite.StringColumn("body_markdown")
		status           = sqlite.StringColumn("status")
		firstPublishedAt = sqlite.StringColumn("first_published_at")
		createdAt        = sqlite.StringColumn("created_at")
		updatedAt        = sqlite.StringColumn("updated_at")
		all              = sqlite.ColumnList{id, orgID, projectID, categoryID, title, slug, bodyMarkdown, status, firstPublishedAt, createdAt, updatedAt}
	)
	return &BlogPosts{
		Table:            sqlite.NewTable("", tablePrefix+"blog_posts", "blog_posts", all...),
		ID:               id,
		OrgID:            orgID,
		ProjectID:        projectID,
		CategoryID:       categoryID,
		Title:            title,
		Slug:             slug,
		BodyMarkdown:     bodyMarkdown,
		Status:           status,
		FirstPublishedAt: firstPublishedAt,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
		AllColumns:       all,
	}
}
