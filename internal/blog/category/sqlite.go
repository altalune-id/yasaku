package category

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

	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

type sqliteStore struct {
	db    *sql.DB
	table *sqliteent.BlogCategories
}

func newSQLiteStore(db *sql.DB, tablePrefix string) *sqliteStore {
	return &sqliteStore{db: db, table: sqliteent.NewBlogCategories(tablePrefix)}
}

type sqliteCategoryRow struct {
	ID        string `alias:"blog_categories.id"`
	OrgID     string `alias:"blog_categories.org_id"`
	ProjectID string `alias:"blog_categories.project_id"`
	Name      string `alias:"blog_categories.name"`
	Slug      string `alias:"blog_categories.slug"`
	CreatedAt string `alias:"blog_categories.created_at"`
	UpdatedAt string `alias:"blog_categories.updated_at"`
}

func (r *sqliteCategoryRow) toCategory() (*Category, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("category.sqlite: parse id: %w", err)
	}
	oid, err := uuid.Parse(r.OrgID)
	if err != nil {
		return nil, fmt.Errorf("category.sqlite: parse org_id: %w", err)
	}
	pid, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("category.sqlite: parse project_id: %w", err)
	}
	ca, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("category.sqlite: parse created_at: %w", err)
	}
	ua, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("category.sqlite: parse updated_at: %w", err)
	}
	return &Category{
		ID:        id,
		OrgID:     oid,
		ProjectID: pid,
		Name:      r.Name,
		Slug:      r.Slug,
		CreatedAt: ca.UTC(),
		UpdatedAt: ua.UTC(),
	}, nil
}

func (s *sqliteStore) Save(ctx context.Context, c *Category) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	updatedAt := sqliteent.SQLiteTime(c.UpdatedAt)
	// SECURITY: the conflict clause is guarded by org, or an upsert carrying another
	// tenant's row id would overwrite that row. SQLite has no RLS behind this.
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			c.ID.String(),
			c.OrgID.String(),
			c.ProjectID.String(),
			c.Name,
			c.Slug,
			sqliteent.SQLiteTime(c.CreatedAt),
			updatedAt,
		).
		ON_CONFLICT(s.table.ID).
		DO_UPDATE(
			sqlite.SET(
				s.table.Name.SET(sqlite.String(c.Name)),
				s.table.Slug.SET(sqlite.String(c.Slug)),
				s.table.UpdatedAt.SET(sqlite.String(updatedAt)),
			).WHERE(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))),
		)
	res, execErr := stmt.ExecContext(ctx, s.db)
	if execErr != nil {
		if typed := translateSQLiteError(execErr, c.Slug, c.ID.String()); typed != nil {
			return typed
		}
		return fmt.Errorf("category.sqlite.Save: %w", execErr)
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("category.sqlite.Save: rows affected: %w", raErr)
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return &NotFoundError{ID: c.ID.String()}
	}
	return nil
}

func (s *sqliteStore) ByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String())))).
		LIMIT(1)
	var row sqliteCategoryRow
	if qErr := stmt.QueryContext(ctx, s.db, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("category.sqlite.ByID: %w", qErr)
	}
	return row.toCategory()
}

func (s *sqliteStore) ByIDs(ctx context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Category, error) {
	if _, err := tenant.From(ctx); err != nil {
		return nil, err
	}
	out := map[uuid.UUID]*Category{}
	if len(ids) == 0 {
		return out, nil
	}
	wanted := make([]sqlite.Expression, 0, len(ids))
	for _, id := range ids {
		wanted = append(wanted, sqlite.String(id.String()))
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.table.ProjectID.EQ(sqlite.String(projectID.String()))).
			AND(s.table.ID.IN(wanted...)))
	var rows []sqliteCategoryRow
	if qErr := stmt.QueryContext(ctx, s.db, &rows); qErr != nil {
		return nil, fmt.Errorf("category.sqlite.ByIDs: %w", qErr)
	}
	for i := range rows {
		c, cErr := rows[i].toCategory()
		if cErr != nil {
			return nil, cErr
		}
		out[c.ID] = c
	}
	return out, nil
}

func (s *sqliteStore) List(ctx context.Context, orgID, projectID uuid.UUID) ([]*Category, error) {
	if _, err := tenant.From(ctx); err != nil {
		return nil, err
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.table.ProjectID.EQ(sqlite.String(projectID.String())))).
		ORDER_BY(s.table.CreatedAt.DESC(), s.table.ID.DESC())
	var rows []sqliteCategoryRow
	if qErr := stmt.QueryContext(ctx, s.db, &rows); qErr != nil {
		return nil, fmt.Errorf("category.sqlite.List: %w", qErr)
	}
	out := make([]*Category, 0, len(rows))
	for i := range rows {
		c, cErr := rows[i].toCategory()
		if cErr != nil {
			return nil, cErr
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *sqliteStore) Delete(ctx context.Context, id uuid.UUID) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	stmt := s.table.DELETE().
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))))
	res, execErr := stmt.ExecContext(ctx, s.db)
	if execErr != nil {
		if typed := translateSQLiteError(execErr, "", id.String()); typed != nil {
			return typed
		}
		return fmt.Errorf("category.sqlite.Delete: %w", execErr)
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("category.sqlite.Delete: rows affected: %w", raErr)
	}
	if n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}

func translateSQLiteError(err error, slug, id string) error {
	var sqliteErr *sqlitedrv.Error
	if !errors.As(err, &sqliteErr) {
		return nil
	}
	switch sqliteErr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return &AlreadyExistsError{Slug: slug}
	// NOTE: SQLite raises an ON DELETE RESTRICT violation as SQLITE_CONSTRAINT_TRIGGER
	// (FK actions run as internal triggers); only insert-side violations use FOREIGNKEY.
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY, sqlite3.SQLITE_CONSTRAINT_TRIGGER:
		return &InUseError{ID: id}
	}
	return nil
}
