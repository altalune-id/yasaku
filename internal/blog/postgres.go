package blog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	pdb "altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	"altalune.id/yasaku/internal/platform/tenant"
)

type postgresStore struct {
	pool  pdb.Pool
	pc    *tenant.PgConn
	posts *pgent.BlogPosts
	links *pgent.BlogPostTags
}

func newPostgresStore(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresStore {
	return &postgresStore{
		pool:  pool,
		pc:    pc,
		posts: pgent.NewBlogPosts(schema, tablePrefix),
		links: pgent.NewBlogPostTags(schema, tablePrefix),
	}
}

type pgPostRow struct {
	ID               uuid.UUID  `alias:"blog_posts.id"`
	OrgID            uuid.UUID  `alias:"blog_posts.org_id"`
	ProjectID        uuid.UUID  `alias:"blog_posts.project_id"`
	CategoryID       uuid.UUID  `alias:"blog_posts.category_id"`
	Title            string     `alias:"blog_posts.title"`
	Slug             string     `alias:"blog_posts.slug"`
	BodyMarkdown     string     `alias:"blog_posts.body_markdown"`
	Status           string     `alias:"blog_posts.status"`
	FirstPublishedAt *time.Time `alias:"blog_posts.first_published_at"`
	CreatedAt        time.Time  `alias:"blog_posts.created_at"`
	UpdatedAt        time.Time  `alias:"blog_posts.updated_at"`
}

func (r *pgPostRow) toPost() *Post {
	p := &Post{
		ID:           r.ID,
		OrgID:        r.OrgID,
		ProjectID:    r.ProjectID,
		CategoryID:   r.CategoryID,
		TagIDs:       []uuid.UUID{},
		Title:        r.Title,
		Slug:         r.Slug,
		BodyMarkdown: r.BodyMarkdown,
		Status:       Status(r.Status),
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	if r.FirstPublishedAt != nil {
		t := r.FirstPublishedAt.UTC()
		p.FirstPublishedAt = &t
	}
	return p
}

type pgLinkRow struct {
	PostID uuid.UUID `alias:"blog_post_tags.post_id"`
	TagID  uuid.UUID `alias:"blog_post_tags.tag_id"`
}

type pgCountRow struct {
	Key   uuid.UUID `alias:"counts.key"`
	Total int64     `alias:"counts.total"`
}

func (s *postgresStore) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		tc, err := tenant.From(ctx)
		if err != nil {
			return nil, false, tenant.Context{}, err
		}
		return tx, false, tc, nil
	}
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	tx, err := s.pc.BeginTenanted(ctx, tc)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("blog.postgres: begin: %w", err)
	}
	return tx, true, tc, nil
}

func (s *postgresStore) endTx(tx *sql.Tx, owned bool, err error) error {
	if !owned {
		return err
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if cerr := tx.Commit(); cerr != nil {
		return fmt.Errorf("blog.postgres: commit: %w", cerr)
	}
	return nil
}

func (s *postgresStore) Save(ctx context.Context, p *Post) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	return s.endTx(tx, owned, s.save(ctx, tx, tc, p))
}

func (s *postgresStore) save(ctx context.Context, tx *sql.Tx, tc tenant.Context, p *Post) error {
	// SECURITY: the conflict clause is guarded by org, or an upsert carrying another
	// tenant's row id would rewrite that row wherever RLS is inert.
	stmt := s.posts.INSERT(s.posts.AllColumns).
		VALUES(
			p.ID, p.OrgID, p.ProjectID, p.CategoryID,
			p.Title, p.Slug, p.BodyMarkdown, string(p.Status),
			pgNullableTime(p.FirstPublishedAt),
			p.CreatedAt.UTC(), p.UpdatedAt.UTC(),
		).
		ON_CONFLICT(s.posts.ID).
		DO_UPDATE(
			postgres.SET(
				s.posts.CategoryID.SET(postgres.UUID(p.CategoryID)),
				s.posts.Title.SET(postgres.String(p.Title)),
				s.posts.Slug.SET(postgres.String(p.Slug)),
				s.posts.BodyMarkdown.SET(postgres.String(p.BodyMarkdown)),
				s.posts.Status.SET(postgres.String(string(p.Status))),
				s.posts.FirstPublishedAt.SET(pgNullableTimeExpr(p.FirstPublishedAt)),
				s.posts.UpdatedAt.SET(postgres.TimestampzT(p.UpdatedAt.UTC())),
			).WHERE(s.posts.OrgID.EQ(postgres.UUID(tc.OrgID))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if mapped := mapPgConstraint(execErr, p.Slug); mapped != nil {
			return mapped
		}
		return fmt.Errorf("blog.postgres.Save: %w", execErr)
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("blog.postgres.Save: rows affected: %w", raErr)
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
func (s *postgresStore) replaceLinks(ctx context.Context, tx *sql.Tx, tc tenant.Context, p *Post) error {
	del := s.links.DELETE().
		WHERE(s.links.PostID.EQ(postgres.UUID(p.ID)).
			AND(s.links.OrgID.EQ(postgres.UUID(tc.OrgID))))
	if _, err := del.ExecContext(ctx, tx); err != nil {
		return fmt.Errorf("blog.postgres.Save: clear tags: %w", err)
	}
	if len(p.TagIDs) == 0 {
		return nil
	}
	ins := s.links.INSERT(s.links.AllColumns)
	for _, id := range p.TagIDs {
		ins = ins.VALUES(p.ID, id, tc.OrgID)
	}
	if _, err := ins.ExecContext(ctx, tx); err != nil {
		return fmt.Errorf("blog.postgres.Save: attach tags: %w", err)
	}
	return nil
}

func (s *postgresStore) ByID(ctx context.Context, id uuid.UUID) (*Post, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(s.posts.AllColumns).
		FROM(s.posts).
		WHERE(s.posts.ID.EQ(postgres.UUID(id)).
			AND(s.posts.OrgID.EQ(postgres.UUID(tc.OrgID)))).
		LIMIT(1)
	var row pgPostRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("blog.postgres.ByID: %w", qErr)
	}
	p := row.toPost()
	byPost, err := s.loadLinks(ctx, tx, tc, []uuid.UUID{p.ID}, "ByID")
	if err != nil {
		return nil, err
	}
	p.TagIDs = byPost[p.ID]
	return p, nil
}

func (s *postgresStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Post, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	cond := s.posts.OrgID.EQ(postgres.UUID(orgID)).
		AND(s.posts.OrgID.EQ(postgres.UUID(tc.OrgID))).
		AND(s.posts.ProjectID.EQ(postgres.UUID(projectID)))
	if opts.Status != nil {
		cond = cond.AND(s.posts.Status.EQ(postgres.String(string(*opts.Status))))
	}
	if opts.CategoryID != nil {
		cond = cond.AND(s.posts.CategoryID.EQ(postgres.UUID(*opts.CategoryID)))
	}
	stmt := postgres.SELECT(s.posts.AllColumns).
		FROM(s.posts).
		WHERE(cond).
		ORDER_BY(s.posts.CreatedAt.DESC(), s.posts.ID.DESC())
	var rows []pgPostRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("blog.postgres.List: %w", qErr)
	}
	out := make([]*Post, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for i := range rows {
		p := rows[i].toPost()
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

func (s *postgresStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	return s.endTx(tx, owned, s.deletePost(ctx, tx, tc, id))
}

func (s *postgresStore) deletePost(ctx context.Context, tx *sql.Tx, tc tenant.Context, id uuid.UUID) error {
	del := s.links.DELETE().
		WHERE(s.links.PostID.EQ(postgres.UUID(id)).
			AND(s.links.OrgID.EQ(postgres.UUID(tc.OrgID))))
	if _, err := del.ExecContext(ctx, tx); err != nil {
		return fmt.Errorf("blog.postgres.Delete: clear tags: %w", err)
	}
	stmt := s.posts.DELETE().
		WHERE(s.posts.ID.EQ(postgres.UUID(id)).
			AND(s.posts.OrgID.EQ(postgres.UUID(tc.OrgID))))
	res, err := stmt.ExecContext(ctx, tx)
	if err != nil {
		return fmt.Errorf("blog.postgres.Delete: %w", err)
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("blog.postgres.Delete: rows affected: %w", raErr)
	}
	if n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}

func (s *postgresStore) CountByCategory(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(
		s.posts.CategoryID.AS("counts.key"),
		postgres.COUNT(postgres.STAR).AS("counts.total"),
	).
		FROM(s.posts).
		WHERE(s.posts.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.posts.OrgID.EQ(postgres.UUID(tc.OrgID))).
			AND(s.posts.ProjectID.EQ(postgres.UUID(projectID)))).
		GROUP_BY(s.posts.CategoryID)
	return s.scanCounts(ctx, tx, stmt, "CountByCategory")
}

func (s *postgresStore) CountByTag(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(
		s.links.TagID.AS("counts.key"),
		postgres.COUNT(postgres.STAR).AS("counts.total"),
	).
		FROM(s.links.INNER_JOIN(s.posts,
			s.posts.ID.EQ(s.links.PostID).AND(s.posts.OrgID.EQ(s.links.OrgID)))).
		WHERE(s.posts.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.posts.OrgID.EQ(postgres.UUID(tc.OrgID))).
			AND(s.links.OrgID.EQ(postgres.UUID(tc.OrgID))).
			AND(s.posts.ProjectID.EQ(postgres.UUID(projectID)))).
		GROUP_BY(s.links.TagID)
	return s.scanCounts(ctx, tx, stmt, "CountByTag")
}

func (s *postgresStore) scanCounts(ctx context.Context, tx *sql.Tx, stmt postgres.SelectStatement, op string) (map[uuid.UUID]int, error) {
	var rows []pgCountRow
	if err := stmt.QueryContext(ctx, tx, &rows); err != nil {
		return nil, fmt.Errorf("blog.postgres.%s: %w", op, err)
	}
	out := make(map[uuid.UUID]int, len(rows))
	for _, r := range rows {
		out[r.Key] = int(r.Total)
	}
	return out, nil
}

// loadLinks reads the tag ids of the given posts in one keyed query, so a post's tags never
// multiply its row in the parent select.
func (s *postgresStore) loadLinks(ctx context.Context, tx *sql.Tx, tc tenant.Context, ids []uuid.UUID, op string) (map[uuid.UUID][]uuid.UUID, error) {
	out := map[uuid.UUID][]uuid.UUID{}
	if len(ids) == 0 {
		return out, nil
	}
	vals := make([]postgres.Expression, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, postgres.UUID(id))
	}
	stmt := postgres.SELECT(s.links.PostID, s.links.TagID).
		FROM(s.links).
		WHERE(s.links.PostID.IN(vals...).
			AND(s.links.OrgID.EQ(postgres.UUID(tc.OrgID)))).
		ORDER_BY(s.links.PostID.ASC(), s.links.TagID.ASC())
	var rows []pgLinkRow
	if err := stmt.QueryContext(ctx, tx, &rows); err != nil {
		return nil, fmt.Errorf("blog.postgres.%s: load tags: %w", op, err)
	}
	for _, r := range rows {
		out[r.PostID] = append(out[r.PostID], r.TagID)
	}
	return out, nil
}

func pgNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func pgNullableTimeExpr(t *time.Time) postgres.TimestampzExpression {
	if t == nil {
		return postgres.TimestampzExp(postgres.NULL)
	}
	return postgres.TimestampzT(t.UTC())
}

func mapPgConstraint(err error, slug string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	if pgErr.Code == "23505" {
		return &AlreadyExistsError{Slug: slug}
	}
	return nil
}
