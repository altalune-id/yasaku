package postgres

import "github.com/go-jet/jet/v2/postgres"

// BlogPosts is the jet binding for the blog_posts table.
type BlogPosts struct {
	postgres.Table

	ID               postgres.ColumnString
	OrgID            postgres.ColumnString
	ProjectID        postgres.ColumnString
	CategoryID       postgres.ColumnString
	Title            postgres.ColumnString
	Slug             postgres.ColumnString
	BodyMarkdown     postgres.ColumnString
	Status           postgres.ColumnString
	FirstPublishedAt postgres.ColumnTimestampz
	CreatedAt        postgres.ColumnTimestampz
	UpdatedAt        postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewBlogPosts builds the blog_posts binding.
func NewBlogPosts(schema, tablePrefix string) *BlogPosts {
	if schema == "" {
		schema = "public"
	}
	var (
		id               = postgres.StringColumn("id")
		orgID            = postgres.StringColumn("org_id")
		projectID        = postgres.StringColumn("project_id")
		categoryID       = postgres.StringColumn("category_id")
		title            = postgres.StringColumn("title")
		slug             = postgres.StringColumn("slug")
		bodyMarkdown     = postgres.StringColumn("body_markdown")
		status           = postgres.StringColumn("status")
		firstPublishedAt = postgres.TimestampzColumn("first_published_at")
		createdAt        = postgres.TimestampzColumn("created_at")
		updatedAt        = postgres.TimestampzColumn("updated_at")
		all              = postgres.ColumnList{id, orgID, projectID, categoryID, title, slug, bodyMarkdown, status, firstPublishedAt, createdAt, updatedAt}
	)
	return &BlogPosts{
		Table:            postgres.NewTable(schema, tablePrefix+"blog_posts", "blog_posts", all...),
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
