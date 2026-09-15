package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// BlogPostTags is the jet binding for the blog_post_tags table.
type BlogPostTags struct {
	sqlite.Table

	PostID sqlite.ColumnString
	TagID  sqlite.ColumnString
	OrgID  sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewBlogPostTags builds the blog_post_tags binding.
func NewBlogPostTags(tablePrefix string) *BlogPostTags {
	var (
		postID = sqlite.StringColumn("post_id")
		tagID  = sqlite.StringColumn("tag_id")
		orgID  = sqlite.StringColumn("org_id")
		all    = sqlite.ColumnList{postID, tagID, orgID}
	)
	return &BlogPostTags{
		Table:      sqlite.NewTable("", tablePrefix+"blog_post_tags", "blog_post_tags", all...),
		PostID:     postID,
		TagID:      tagID,
		OrgID:      orgID,
		AllColumns: all,
	}
}
