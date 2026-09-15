package tag

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
	table *pgent.BlogTags
}

func newPostgresStore(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresStore {
	return &postgresStore{pool: pool, pc: pc, table: pgent.NewBlogTags(schema, tablePrefix)}
}

type pgTagRow struct {
	ID        uuid.UUID `alias:"blog_tags.id"`
	OrgID     uuid.UUID `alias:"blog_tags.org_id"`
	ProjectID uuid.UUID `alias:"blog_tags.project_id"`
	Name      string    `alias:"blog_tags.name"`
	Slug      string    `alias:"blog_tags.slug"`
	CreatedAt time.Time `alias:"blog_tags.created_at"`
	UpdatedAt time.Time `alias:"blog_tags.updated_at"`
}

func (r *pgTagRow) toTag() *Tag {
	return &Tag{
		ID:        r.ID,
		OrgID:     r.OrgID,
		ProjectID: r.ProjectID,
		Name:      r.Name,
		Slug:      r.Slug,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func (s *postgresStore) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		tc, _ := tenant.From(ctx)
		return tx, false, tc, nil
	}
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	tx, err := s.pc.BeginTenanted(ctx, tc)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("tag.postgres: begin: %w", err)
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
		return fmt.Errorf("tag.postgres: commit: %w", cerr)
	}
	return nil
}

func (s *postgresStore) Save(ctx context.Context, t *Tag) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			t.ID, t.OrgID, t.ProjectID, t.Name, t.Slug,
			t.CreatedAt.UTC(), t.UpdatedAt.UTC(),
		).
		ON_CONFLICT(s.table.ID).
		DO_UPDATE(
			postgres.SET(
				s.table.Name.SET(postgres.String(t.Name)),
				s.table.Slug.SET(postgres.String(t.Slug)),
				s.table.UpdatedAt.SET(postgres.TimestampzT(t.UpdatedAt.UTC())),
			).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if mapped := mapPgConstraint(execErr, t.Slug, t.ID.String()); mapped != nil {
			return s.endTx(tx, owned, mapped)
		}
		return s.endTx(tx, owned, fmt.Errorf("tag.postgres.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("tag.postgres.Save: rows affected: %w", raErr))
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: t.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) ByID(ctx context.Context, id uuid.UUID) (*Tag, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ID.EQ(postgres.UUID(id)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID)))).
		LIMIT(1)
	var row pgTagRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("tag.postgres.ByID: %w", qErr)
	}
	return row.toTag(), nil
}

func (s *postgresStore) BySlug(ctx context.Context, orgID, projectID uuid.UUID, slug string) (*Tag, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))).
			AND(s.table.ProjectID.EQ(postgres.UUID(projectID))).
			AND(s.table.Slug.EQ(postgres.String(slug)))).
		LIMIT(1)
	var row pgTagRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: slug}
		}
		return nil, fmt.Errorf("tag.postgres.BySlug: %w", qErr)
	}
	return row.toTag(), nil
}

func (s *postgresStore) ByIDs(ctx context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Tag, error) {
	out := map[uuid.UUID]*Tag{}
	if len(ids) == 0 {
		if _, err := tenant.From(ctx); err != nil {
			return nil, err
		}
		return out, nil
	}
	vals := make([]postgres.Expression, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, postgres.UUID(id))
	}
	rows, err := s.queryMany(ctx,
		s.table.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.table.ProjectID.EQ(postgres.UUID(projectID))).
			AND(s.table.ID.IN(vals...)),
		"ByIDs")
	if err != nil {
		return nil, err
	}
	for _, t := range rows {
		out[t.ID] = t
	}
	return out, nil
}

func (s *postgresStore) List(ctx context.Context, orgID, projectID uuid.UUID) ([]*Tag, error) {
	return s.queryMany(ctx,
		s.table.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.table.ProjectID.EQ(postgres.UUID(projectID))),
		"List")
}

func (s *postgresStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	stmt := s.table.DELETE().
		WHERE(s.table.ID.EQ(postgres.UUID(id)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))))
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if mapped := mapPgConstraint(execErr, "", id.String()); mapped != nil {
			return s.endTx(tx, owned, mapped)
		}
		return s.endTx(tx, owned, fmt.Errorf("tag.postgres.Delete: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("tag.postgres.Delete: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: id.String()})
	}
	return s.endTx(tx, owned, nil)
}

// queryMany pins every multi-row read to the caller's tenant on top of the caller-supplied
// scope, so a mismatched orgID argument reads nothing even where RLS is not enforced.
func (s *postgresStore) queryMany(ctx context.Context, cond postgres.BoolExpression, op string) ([]*Tag, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(cond.AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID)))).
		ORDER_BY(s.table.CreatedAt.DESC(), s.table.ID.DESC())
	var rows []pgTagRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("tag.postgres.%s: %w", op, qErr)
	}
	out := make([]*Tag, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toTag())
	}
	return out, nil
}

func mapPgConstraint(err error, slug, id string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23505":
		return &AlreadyExistsError{Slug: slug}
	case "23503":
		return &InUseError{ID: id}
	}
	return nil
}
