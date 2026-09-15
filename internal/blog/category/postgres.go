package category

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
	table *pgent.BlogCategories
}

func newPostgresStore(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresStore {
	return &postgresStore{pool: pool, pc: pc, table: pgent.NewBlogCategories(schema, tablePrefix)}
}

type pgCategoryRow struct {
	ID        uuid.UUID `alias:"blog_categories.id"`
	OrgID     uuid.UUID `alias:"blog_categories.org_id"`
	ProjectID uuid.UUID `alias:"blog_categories.project_id"`
	Name      string    `alias:"blog_categories.name"`
	Slug      string    `alias:"blog_categories.slug"`
	CreatedAt time.Time `alias:"blog_categories.created_at"`
	UpdatedAt time.Time `alias:"blog_categories.updated_at"`
}

func (r *pgCategoryRow) toCategory() *Category {
	return &Category{
		ID:        r.ID,
		OrgID:     r.OrgID,
		ProjectID: r.ProjectID,
		Name:      r.Name,
		Slug:      r.Slug,
		CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
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
		return nil, false, tenant.Context{}, fmt.Errorf("category.postgres: begin: %w", err)
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
		return fmt.Errorf("category.postgres: commit: %w", cerr)
	}
	return nil
}

func (s *postgresStore) Save(ctx context.Context, c *Category) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: the conflict clause is guarded by org, or an upsert carrying another
	// tenant's row id would overwrite that row.
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			c.ID, c.OrgID, c.ProjectID, c.Name, c.Slug,
			c.CreatedAt.UTC(), c.UpdatedAt.UTC(),
		).
		ON_CONFLICT(s.table.ID).
		DO_UPDATE(
			postgres.SET(
				s.table.Name.SET(postgres.String(c.Name)),
				s.table.Slug.SET(postgres.String(c.Slug)),
				s.table.UpdatedAt.SET(postgres.TimestampzT(c.UpdatedAt.UTC())),
			).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translatePgError(execErr, c.Slug, c.ID.String()); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("category.postgres.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("category.postgres.Save: rows affected: %w", raErr))
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: c.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) ByID(ctx context.Context, id uuid.UUID) (*Category, error) {
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
	var row pgCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("category.postgres.ByID: %w", qErr)
	}
	return row.toCategory(), nil
}

func (s *postgresStore) ByIDs(ctx context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Category, error) {
	out := map[uuid.UUID]*Category{}
	if len(ids) == 0 {
		if _, err := tenant.From(ctx); err != nil {
			return nil, err
		}
		return out, nil
	}
	tx, owned, _, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	wanted := make([]postgres.Expression, 0, len(ids))
	for _, id := range ids {
		wanted = append(wanted, postgres.UUID(id))
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.table.ProjectID.EQ(postgres.UUID(projectID))).
			AND(s.table.ID.IN(wanted...)))
	var rows []pgCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("category.postgres.ByIDs: %w", qErr)
	}
	for i := range rows {
		c := rows[i].toCategory()
		out[c.ID] = c
	}
	return out, nil
}

func (s *postgresStore) List(ctx context.Context, orgID, projectID uuid.UUID) ([]*Category, error) {
	tx, owned, _, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.table.ProjectID.EQ(postgres.UUID(projectID)))).
		ORDER_BY(s.table.CreatedAt.DESC(), s.table.ID.DESC())
	var rows []pgCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("category.postgres.List: %w", qErr)
	}
	out := make([]*Category, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toCategory())
	}
	return out, nil
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
		if typed := translatePgError(execErr, "", id.String()); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("category.postgres.Delete: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("category.postgres.Delete: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: id.String()})
	}
	return s.endTx(tx, owned, nil)
}

func translatePgError(err error, slug, id string) error {
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
