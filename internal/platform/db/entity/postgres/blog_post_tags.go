package postgres

import "github.com/go-jet/jet/v2/postgres"

// BlogPostTags is the jet binding for the blog_post_tags table.
type BlogPostTags struct {
	postgres.Table

	PostID postgres.ColumnString
	TagID  postgres.ColumnString
	OrgID  postgres.ColumnString

	AllColumns postgres.ColumnList
}

// NewBlogPostTags builds the blog_post_tags binding.
func NewBlogPostTags(schema, tablePrefix string) *BlogPostTags {
	if schema == "" {
		schema = "public"
	}
	var (
		postID = postgres.StringColumn("post_id")
		tagID  = postgres.StringColumn("tag_id")
		orgID  = postgres.StringColumn("org_id")
		all    = postgres.ColumnList{postID, tagID, orgID}
	)
	return &BlogPostTags{
		Table:      postgres.NewTable(schema, tablePrefix+"blog_post_tags", "blog_post_tags", all...),
		PostID:     postID,
		TagID:      tagID,
		OrgID:      orgID,
		AllColumns: all,
	}
}
