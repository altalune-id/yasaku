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

	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

type sqliteStore struct {
	db    *sql.DB
	table *sqliteent.Categories
}

func newSQLiteStore(db *sql.DB, tablePrefix string) *sqliteStore {
	return &sqliteStore{db: db, table: sqliteent.NewCategories(tablePrefix)}
}

type sqliteCategoryRow struct {
	ID         string  `alias:"categories.id"`
	OrgID      string  `alias:"categories.org_id"`
	ProjectID  string  `alias:"categories.project_id"`
	Name       string  `alias:"categories.name"`
	Kind       string  `alias:"categories.kind"`
	Icon       string  `alias:"categories.icon"`
	Color      string  `alias:"categories.color"`
	SortOrder  int64   `alias:"categories.sort_order"`
	ArchivedAt *string `alias:"categories.archived_at"`
	CreatedAt  string  `alias:"categories.created_at"`
	UpdatedAt  string  `alias:"categories.updated_at"`
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
	c := &Category{
		ID:        id,
		OrgID:     oid,
		ProjectID: pid,
		Name:      r.Name,
		Kind:      Kind(r.Kind),
		Icon:      r.Icon,
		Color:     r.Color,
		SortOrder: int(r.SortOrder),
		CreatedAt: ca.UTC(),
		UpdatedAt: ua.UTC(),
	}
	if r.ArchivedAt != nil && *r.ArchivedAt != "" {
		aa, aErr := time.Parse(time.RFC3339Nano, *r.ArchivedAt)
		if aErr != nil {
			return nil, fmt.Errorf("category.sqlite: parse archived_at: %w", aErr)
		}
		at := aa.UTC()
		c.ArchivedAt = &at
	}
	return c, nil
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
		return nil, false, tenant.Context{}, fmt.Errorf("category.sqlite: begin: %w", err)
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
		return fmt.Errorf("category.sqlite: commit: %w", cerr)
	}
	return nil
}

func (s *sqliteStore) archivedAtExpr(c *Category) sqlite.StringExpression {
	if c.ArchivedAt == nil {
		return sqlite.StringExp(sqlite.NULL)
	}
	return sqlite.String(sqliteent.SQLiteTime(*c.ArchivedAt))
}

func (s *sqliteStore) Save(ctx context.Context, c *Category) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	updatedAt := sqliteent.SQLiteTime(c.UpdatedAt)
	archivedAt := s.archivedAtExpr(c)
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			c.ID.String(),
			c.OrgID.String(),
			c.ProjectID.String(),
			c.Name,
			string(c.Kind),
			c.Icon,
			c.Color,
			int64(SortOrder32(c.SortOrder)),
			archivedAt,
			sqliteent.SQLiteTime(c.CreatedAt),
			updatedAt,
		).
		ON_CONFLICT(s.table.ID).
		// SECURITY: SQLite has no RLS, so this tenant predicate is the only thing stopping an attacker-supplied row id from updating another org's row.
		DO_UPDATE(
			sqlite.SET(
				s.table.Name.SET(sqlite.String(c.Name)),
				s.table.Kind.SET(sqlite.String(string(c.Kind))),
				s.table.Icon.SET(sqlite.String(c.Icon)),
				s.table.Color.SET(sqlite.String(c.Color)),
				s.table.SortOrder.SET(sqlite.Int(int64(SortOrder32(c.SortOrder)))),
				s.table.ArchivedAt.SET(archivedAt),
				s.table.UpdatedAt.SET(sqlite.String(updatedAt)),
			).WHERE(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translateSQLiteError(execErr, c, c.ID.String()); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("category.sqlite.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("category.sqlite.Save: rows affected: %w", raErr))
	}
	// NOTE: zero means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: c.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) ByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String())))).
		LIMIT(1)
	var row sqliteCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: id.String()}
		}
		return nil, fmt.Errorf("category.sqlite.ByID: %w", qErr)
	}
	return row.toCategory()
}

func (s *sqliteStore) ByIDs(ctx context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Category, error) {
	out := map[uuid.UUID]*Category{}
	if len(ids) == 0 {
		if _, err := tenant.From(ctx); err != nil {
			return nil, err
		}
		return out, nil
	}
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	wanted := make([]sqlite.Expression, 0, len(ids))
	for _, id := range ids {
		wanted = append(wanted, sqlite.String(id.String()))
	}
	// SECURITY: both org predicates are layered, as on Postgres, so a caller naming another org's scope matches nothing.
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(s.table.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
			AND(s.table.ProjectID.EQ(sqlite.String(projectID.String()))).
			AND(s.table.ID.IN(wanted...)))
	var rows []sqliteCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
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

func (s *sqliteStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Category, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	// SECURITY: both org predicates are layered, as on Postgres, so a caller naming another org's scope matches nothing.
	where := s.table.OrgID.EQ(sqlite.String(orgID.String())).
		AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
		AND(s.table.ProjectID.EQ(sqlite.String(projectID.String())))
	if opts.Kind != "" {
		where = where.AND(s.table.Kind.EQ(sqlite.String(string(opts.Kind))))
	}
	if !opts.IncludeArchived {
		where = where.AND(s.table.ArchivedAt.IS_NULL())
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(where).
		ORDER_BY(s.table.SortOrder.ASC(), s.table.Name.ASC(), s.table.ID.ASC())
	var rows []sqliteCategoryRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
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
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: org predicate, not RLS — SQLite has none, so this is the only guard on the delete path.
	stmt := s.table.DELETE().
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))))
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translateSQLiteError(execErr, nil, id.String()); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("category.sqlite.Delete: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("category.sqlite.Delete: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: id.String()})
	}
	return s.endTx(tx, owned, nil)
}

func translateSQLiteError(err error, c *Category, id string) error {
	var sqliteErr *sqlitedrv.Error
	if !errors.As(err, &sqliteErr) {
		return nil
	}
	switch sqliteErr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		if c != nil {
			return &AlreadyExistsError{Name: c.Name, Kind: c.Kind}
		}
		return &AlreadyExistsError{}
	// NOTE: SQLite raises an ON DELETE RESTRICT violation as SQLITE_CONSTRAINT_TRIGGER
	// (FK actions run as internal triggers); only insert-side violations use FOREIGNKEY.
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY, sqlite3.SQLITE_CONSTRAINT_TRIGGER:
		return &InUseError{ID: id}
	}
	return nil
}
