package blog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"
	sqlitedrv "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

type sqliteStore struct {
	db    *sql.DB
	posts *sqliteent.BlogPosts
	links *sqliteent.BlogPostTags
}

func newSQLiteStore(sqlDB *sql.DB, tablePrefix string) *sqliteStore {
	return &sqliteStore{
		db:    sqlDB,
		posts: sqliteent.NewBlogPosts(tablePrefix),
		links: sqliteent.NewBlogPostTags(tablePrefix),
	}
}

type sqlitePostRow struct {
	ID               string  `alias:"blog_posts.id"`
	OrgID            string  `alias:"blog_posts.org_id"`
	ProjectID        string  `alias:"blog_posts.project_id"`
	CategoryID       string  `alias:"blog_posts.category_id"`
	Title            string  `alias:"blog_posts.title"`
	Slug             string  `alias:"blog_posts.slug"`
	BodyMarkdown     string  `alias:"blog_posts.body_markdown"`
	Status           string  `alias:"blog_posts.status"`
	FirstPublishedAt *string `alias:"blog_posts.first_published_at"`
	CreatedAt        string  `alias:"blog_posts.created_at"`
	UpdatedAt        string  `alias:"blog_posts.updated_at"`
}

func (r *sqlitePostRow) toPost() (*Post, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("blog.sqlite: parse id: %w", err)
	}
	oid, err := uuid.Parse(r.OrgID)
	if err != nil {
		return nil, fmt.Errorf("blog.sqlite: parse org_id: %w", err)
	}
	pid, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("blog.sqlite: parse project_id: %w", err)
	}
	cid, err := uuid.Parse(r.CategoryID)
	if err != nil {
		return nil, fmt.Errorf("blog.sqlite: parse category_id: %w", err)
	}
	ca, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("blog.sqlite: parse created_at: %w", err)
	}
	ua, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("blog.sqlite: parse updated_at: %w", err)
	}
	p := &Post{
		ID:           id,
		OrgID:        oid,
		ProjectID:    pid,
		CategoryID:   cid,
		TagIDs:       []uuid.UUID{},
		Title:        r.Title,
		Slug:         r.Slug,
		BodyMarkdown: r.BodyMarkdown,
		Status:       Status(r.Status),
		CreatedAt:    ca,
		UpdatedAt:    ua,
	}
	if r.FirstPublishedAt != nil && *r.FirstPublishedAt != "" {
		fp, perr := time.Parse(time.RFC3339Nano, *r.FirstPublishedAt)
		if perr != nil {
			return nil, fmt.Errorf("blog.sqlite: parse first_published_at: %w", perr)
		}
		p.FirstPublishedAt = &fp
	}
	return p, nil
}

type sqliteLinkRow struct {
	PostID string `alias:"blog_post_tags.post_id"`
	TagID  string `alias:"blog_post_tags.tag_id"`
}

type sqliteCountRow struct {
	Key   string `alias:"counts.key"`
	Total int64  `alias:"counts.total"`
}

// txAcquire enrolls in the caller's unit of work when one is active, so a Save never opens a
// second writer transaction against the same SQLite file.
func (s *sqliteStore) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, tc, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("blog.sqlite: begin: %w", err)
	}
	return tx, true, tc, nil
}

func (s *sqliteStore) endTx(tx *sql.Tx, owned bool, err error) error {
	if !owned {
		return err
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if cerr := tx.Commit(); cerr != nil {
		return fmt.Errorf("blog.sqlite: commit: %w", cerr)
	}
	return nil
}

func (s *sqliteStore) Save(ctx context.Context, p *Post) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	return s.endTx(tx, owned, s.save(ctx, tx, tc, p))
}

func (s *sqliteStore) save(ctx context.Context, tx *sql.Tx, tc tenant.Context, p *Post) error {
	updatedAt := sqliteent.SQLiteTime(p.UpdatedAt)
	// SECURITY: the conflict clause is guarded by org, or an upsert carrying another
	// tenant's row id would overwrite that row. SQLite has no RLS behind this.
	stmt := s.posts.INSERT(s.posts.AllColumns).
		VALUES(
			p.ID.String(),
			p.OrgID.String(),
			p.ProjectID.String(),
			p.CategoryID.String(),
			p.Title,
			p.Slug,
			p.BodyMarkdown,
			string(p.Status),
			sqliteNullableTimeArg(p.FirstPublishedAt),
			sqliteent.SQLiteTime(p.CreatedAt),
			updatedAt,
		).
		ON_CONFLICT(s.posts.ID).
		DO_UPDATE(
			sqlite.SET(
				s.posts.CategoryID.SET(sqlite.String(p.CategoryID.String())),
				s.posts.Title.SET(sqlite.String(p.Title)),
				s.posts.Slug.SET(sqlite.String(p.Slug)),
				s.posts.BodyMarkdown.SET(sqlite.String(p.BodyMarkdown)),
				s.posts.Status.SET(sqlite.String(string(p.Status))),
				s.posts.FirstPublishedAt.SET(sqliteNullableTimeExpr(p.FirstPublishedAt)),
				s.posts.UpdatedAt.SET(sqlite.String(updatedAt)),
			).WHERE(s.posts.OrgID.EQ(sqlite.String(tc.OrgID.String()))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if isSQLiteUniqueViolation(execErr) {
			return &AlreadyExistsError{Slug: p.Slug}
		}
		return fmt.Errorf("blog.sqlite.Save: %w", execErr)
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("blog.sqlite.Save: rows affected: %w", raErr)
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return &NotFoundError{ID: p.ID.String()}
	}
	return s.replaceLinks(ctx, tx, tc, p)
}

// replaceLinks swaps the post's join rows wholesale inside the caller's transaction, so a
// partially applied tag set can never be observed.
func (s *sqliteStore) replaceLinks(ctx context.Context, tx *sql.Tx, tc tenant.Context, p *Post) error {
	del := s.links.DELETE().
		WHERE(s.links.PostID.EQ(sqlite.String(p.ID.String())).
			AND(s.links.OrgID.EQ(sqlite.String(tc.OrgID.String()))))
	if _, err := del.ExecContext(ctx, tx); err != nil {
		return fmt.Errorf("blog.sqlite.Save: clear tags: %w", err)
	}
	if len(p.TagIDs) == 0 {
		return nil
	}
	ins := s.links.INSERT(s.links.AllColumns)
	for _, id := range p.TagIDs {
		ins = ins.VALUES(p.ID.String(), id.String(), tc.OrgID.String())
	}
	if _, err := ins.ExecContext(ctx, tx); err != nil {
		return fmt.Errorf("blog.sqlite.Save: attach tags: %w", err)
	}
	return nil
}

func (s *sqliteStore) ByID(ctx context.Context, id uuid.UUID) (*Post, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := sqlite.SELECT(s.posts.AllColumns).
		FROM(s.posts).
		WHERE(s.posts.ID.EQ(sqlite.String(id.String())).
			AND(s.posts.OrgID.EQ(sqlite.String(tc.OrgID.String())))).
		LIMIT(1)
	var row sqlitePostRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("blog.sqlite.ByID: %w", qErr)
	}
	p, err := row.toPost()
	if err != nil {
		return nil, err
	}
	byPost, err := s.loadLinks(ctx, tx, tc, []uuid.UUID{p.ID}, "ByID")
	if err != nil {
		return nil, err
	}
	p.TagIDs = byPost[p.ID]
	return p, nil
}

func (s *sqliteStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Post, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	cond := s.posts.OrgID.EQ(sqlite.String(orgID.String())).
		AND(s.posts.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
		AND(s.posts.ProjectID.EQ(sqlite.String(projectID.String())))
	if opts.Status != nil {
		cond = cond.AND(s.posts.Status.EQ(sqlite.String(string(*opts.Status))))
	}
	if opts.CategoryID != nil {
		cond = cond.AND(s.posts.CategoryID.EQ(sqlite.String(opts.CategoryID.String())))
	}
	stmt := sqlite.SELECT(s.posts.AllColumns).
		FROM(s.posts).
		WHERE(cond).
		ORDER_BY(s.posts.CreatedAt.DESC(), s.posts.ID.DESC())
	var rows []sqlitePostRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("blog.sqlite.List: %w", qErr)
	}
	out := make([]*Post, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		p, cErr := rows[i].toPost()
		if cErr != nil {
			return nil, cErr
		}
		out = append(out, p)
		ids = append(ids, p.ID)
	}
	byPost, err := s.loadLinks(ctx, tx, tc, ids, "List")
	if err != nil {
		return nil, err
	}
	for _, p := range out {
		p.TagIDs = byPost[p.ID]
	}
	return out, nil
}

func (s *sqliteStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	return s.endTx(tx, owned, s.deletePost(ctx, tx, tc, id))
}

func (s *sqliteStore) deletePost(ctx context.Context, tx *sql.Tx, tc tenant.Context, id uuid.UUID) error {
	del := s.links.DELETE().
		WHERE(s.links.PostID.EQ(sqlite.String(id.String())).
			AND(s.links.OrgID.EQ(sqlite.String(tc.OrgID.String()))))
	if _, err := del.ExecContext(ctx, tx); err != nil {
		return fmt.Errorf("blog.sqlite.Delete: clear tags: %w", err)
	}
	stmt := s.posts.DELETE().
		WHERE(s.posts.ID.EQ(sqlite.String(id.String())).
			AND(s.posts.OrgID.EQ(sqlite.String(tc.OrgID.String()))))
	res, err := stmt.ExecContext(ctx, tx)
	if err != nil {
		return fmt.Errorf("blog.sqlite.Delete: %w", err)
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("blog.sqlite.Delete: rows affected: %w", raErr)
	}
	if n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}

func (s *sqliteStore) CountByCategory(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := sqlite.SELECT(
		s.posts.CategoryID.AS("counts.key"),
		sqlite.COUNT(sqlite.STAR).AS("counts.total"),
	).
		FROM(s.posts).
		WHERE(s.posts.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.posts.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
			AND(s.posts.ProjectID.EQ(sqlite.String(projectID.String())))).
		GROUP_BY(s.posts.CategoryID)
	return s.scanCounts(ctx, tx, stmt, "CountByCategory")
}

func (s *sqliteStore) CountByTag(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := sqlite.SELECT(
		s.links.TagID.AS("counts.key"),
		sqlite.COUNT(sqlite.STAR).AS("counts.total"),
	).
		FROM(s.links.INNER_JOIN(s.posts,
			s.posts.ID.EQ(s.links.PostID).AND(s.posts.OrgID.EQ(s.links.OrgID)))).
		WHERE(s.posts.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.posts.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
			AND(s.links.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
			AND(s.posts.ProjectID.EQ(sqlite.String(projectID.String())))).
		GROUP_BY(s.links.TagID)
	return s.scanCounts(ctx, tx, stmt, "CountByTag")
}

func (s *sqliteStore) scanCounts(ctx context.Context, tx *sql.Tx, stmt sqlite.SelectStatement, op string) (map[uuid.UUID]int, error) {
	var rows []sqliteCountRow
	if err := stmt.QueryContext(ctx, tx, &rows); err != nil {
		return nil, fmt.Errorf("blog.sqlite.%s: %w", op, err)
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		key, err := uuid.Parse(r.Key)
		if err != nil {
			return nil, fmt.Errorf("blog.sqlite.%s: parse key: %w", op, err)
		}
		out[key] = int(r.Total)
	}
	return out, nil
}

// loadLinks reads the tag ids of the given posts in one keyed query, so a post's tags never
// multiply its row in the parent select.
func (s *sqliteStore) loadLinks(ctx context.Context, tx *sql.Tx, tc tenant.Context, ids []uuid.UUID, op string) (map[uuid.UUID][]uuid.UUID, error) {
	out := map[uuid.UUID][]uuid.UUID{}
	if len(ids) == 0 {
		return out, nil
	}
	vals := make([]sqlite.Expression, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, sqlite.String(id.String()))
	}
	stmt := sqlite.SELECT(s.links.PostID, s.links.TagID).
		FROM(s.links).
		WHERE(s.links.PostID.IN(vals...).
			AND(s.links.OrgID.EQ(sqlite.String(tc.OrgID.String())))).
		ORDER_BY(s.links.PostID.ASC(), s.links.TagID.ASC())
	var rows []sqliteLinkRow
	if err := stmt.QueryContext(ctx, tx, &rows); err != nil {
		return nil, fmt.Errorf("blog.sqlite.%s: load tags: %w", op, err)
	}
	for _, r := range rows {
		postID, err := uuid.Parse(r.PostID)
		if err != nil {
			return nil, fmt.Errorf("blog.sqlite.%s: parse post_id: %w", op, err)
		}
		tagID, err := uuid.Parse(r.TagID)
		if err != nil {
			return nil, fmt.Errorf("blog.sqlite.%s: parse tag_id: %w", op, err)
		}
		out[postID] = append(out[postID], tagID)
	}
	return out, nil
}

func sqliteNullableTimeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return sqliteent.SQLiteTime(*t)
}

func sqliteNullableTimeExpr(t *time.Time) sqlite.StringExpression {
	if t == nil {
		return sqliteent.NullText()
	}
	return sqlite.String(sqliteent.SQLiteTime(*t))
}

func isSQLiteUniqueViolation(err error) bool {
	var sqliteErr *sqlitedrv.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return true
		}
	}
	return false
}
