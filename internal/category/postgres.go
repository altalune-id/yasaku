package category

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	pdb "altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	"altalune.id/yasaku/internal/platform/tenant"
)

type postgresStore struct {
	pool  pdb.Pool
	pc    *tenant.PgConn
	table *pgent.Categories
}

func newPostgresStore(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresStore {
	return &postgresStore{pool: pool, pc: pc, table: pgent.NewCategories(schema, tablePrefix)}
}

type pgCategoryRow struct {
	ID         uuid.UUID  `alias:"categories.id"`
	OrgID      uuid.UUID  `alias:"categories.org_id"`
	ProjectID  uuid.UUID  `alias:"categories.project_id"`
	Name       string     `alias:"categories.name"`
	Kind       string     `alias:"categories.kind"`
	Icon       string     `alias:"categories.icon"`
	Color      string     `alias:"categories.color"`
	SortOrder  int32      `alias:"categories.sort_order"`
	ArchivedAt *time.Time `alias:"categories.archived_at"`
	CreatedAt  time.Time  `alias:"categories.created_at"`
	UpdatedAt  time.Time  `alias:"categories.updated_at"`
}

func (r *pgCategoryRow) toCategory() *Category {
	c := &Category{
		ID:        r.ID,
		OrgID:     r.OrgID,
		ProjectID: r.ProjectID,
		Name:      r.Name,
		Kind:      Kind(r.Kind),
		Icon:      r.Icon,
		Color:     r.Color,
		SortOrder: int(r.SortOrder),
		CreatedAt: r.CreatedAt.UTC(),
		UpdatedAt: r.UpdatedAt.UTC(),
	}
	if r.ArchivedAt != nil {
		at := r.ArchivedAt.UTC()
		c.ArchivedAt = &at
	}
	return c
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

func translatePgError(err error, c *Category, id string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "23505":
		if c != nil {
			return &AlreadyExistsError{Name: c.Name, Kind: c.Kind}
		}
		return &AlreadyExistsError{}
	case "23503":
		return &InUseError{ID: id}
	}
	return nil
}
