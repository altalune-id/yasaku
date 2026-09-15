package tag

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
	table *sqliteent.BlogTags
}

func newSQLiteStore(db *sql.DB, tablePrefix string) *sqliteStore {
	return &sqliteStore{db: db, table: sqliteent.NewBlogTags(tablePrefix)}
}

type sqliteTagRow struct {
	ID        string `alias:"blog_tags.id"`
	OrgID     string `alias:"blog_tags.org_id"`
	ProjectID string `alias:"blog_tags.project_id"`
	Name      string `alias:"blog_tags.name"`
	Slug      string `alias:"blog_tags.slug"`
	CreatedAt string `alias:"blog_tags.created_at"`
	UpdatedAt string `alias:"blog_tags.updated_at"`
}

func (r *sqliteTagRow) toTag() (*Tag, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("tag.sqlite: parse id: %w", err)
	}
	oid, err := uuid.Parse(r.OrgID)
	if err != nil {
		return nil, fmt.Errorf("tag.sqlite: parse org_id: %w", err)
	}
	pid, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("tag.sqlite: parse project_id: %w", err)
	}
	ca, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("tag.sqlite: parse created_at: %w", err)
	}
	ua, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("tag.sqlite: parse updated_at: %w", err)
	}
	return &Tag{
		ID:        id,
		OrgID:     oid,
		ProjectID: pid,
		Name:      r.Name,
		Slug:      r.Slug,
		CreatedAt: ca,
		UpdatedAt: ua,
	}, nil
}

func (s *sqliteStore) Save(ctx context.Context, t *Tag) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	updatedAt := sqliteent.SQLiteTime(t.UpdatedAt)
	// SECURITY: the conflict clause is guarded by org, or an upsert carrying another
	// tenant's row id would overwrite that row. SQLite has no RLS behind this.
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			t.ID.String(),
			t.OrgID.String(),
			t.ProjectID.String(),
			t.Name,
			t.Slug,
			sqliteent.SQLiteTime(t.CreatedAt),
			updatedAt,
		).
		ON_CONFLICT(s.table.ID).
		DO_UPDATE(
			sqlite.SET(
				s.table.Name.SET(sqlite.String(t.Name)),
				s.table.Slug.SET(sqlite.String(t.Slug)),
				s.table.UpdatedAt.SET(sqlite.String(updatedAt)),
			).WHERE(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))),
		)
	res, execErr := stmt.ExecContext(ctx, s.db)
	if execErr != nil {
		if isSQLiteUniqueViolation(execErr) {
			return &AlreadyExistsError{Slug: t.Slug}
		}
		return fmt.Errorf("tag.sqlite.Save: %w", execErr)
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("tag.sqlite.Save: rows affected: %w", raErr)
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return &NotFoundError{ID: t.ID.String()}
	}
	return nil
}

func (s *sqliteStore) ByID(ctx context.Context, id uuid.UUID) (*Tag, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	return s.queryOne(ctx,
		s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))),
		&NotFoundError{ID: id.String()})
}

func (s *sqliteStore) BySlug(ctx context.Context, orgID, projectID uuid.UUID, slug string) (*Tag, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	if tc.OrgID != orgID {
		return nil, &NotFoundError{ID: slug}
	}
	return s.queryOne(ctx,
		s.table.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.table.ProjectID.EQ(sqlite.String(projectID.String()))).
			AND(s.table.Slug.EQ(sqlite.String(slug))),
		&NotFoundError{ID: slug})
}

func (s *sqliteStore) ByIDs(ctx context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Tag, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]*Tag{}
	if len(ids) == 0 || tc.OrgID != orgID {
		return out, nil
	}
	vals := make([]sqlite.Expression, 0, len(ids))
	for _, id := range ids {
		vals = append(vals, sqlite.String(id.String()))
	}
	rows, err := s.queryMany(ctx,
		s.table.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.table.ProjectID.EQ(sqlite.String(projectID.String()))).
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

func (s *sqliteStore) List(ctx context.Context, orgID, projectID uuid.UUID) ([]*Tag, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	if tc.OrgID != orgID {
		return []*Tag{}, nil
	}
	return s.queryMany(ctx,
		s.table.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.table.ProjectID.EQ(sqlite.String(projectID.String()))),
		"List")
}

func (s *sqliteStore) Delete(ctx context.Context, id uuid.UUID) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	stmt := s.table.DELETE().
		WHERE(s.table.ID.EQ(sqlite.String(id.String())).
			AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))))
	res, err := stmt.ExecContext(ctx, s.db)
	if err != nil {
		if isSQLiteForeignKeyViolation(err) {
			return &InUseError{ID: id.String()}
		}
		return fmt.Errorf("tag.sqlite.Delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("tag.sqlite.Delete: rows affected: %w", err)
	}
	if n == 0 {
		return &NotFoundError{ID: id.String()}
	}
	return nil
}

func (s *sqliteStore) queryOne(ctx context.Context, cond sqlite.BoolExpression, notFound *NotFoundError) (*Tag, error) {
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(cond).
		LIMIT(1)
	var row sqliteTagRow
	if err := stmt.QueryContext(ctx, s.db, &row); err != nil {
		if errors.Is(err, qrm.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, notFound
		}
		return nil, fmt.Errorf("tag.sqlite.queryOne: %w", err)
	}
	return row.toTag()
}

func (s *sqliteStore) queryMany(ctx context.Context, cond sqlite.BoolExpression, op string) ([]*Tag, error) {
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(cond).
		ORDER_BY(s.table.CreatedAt.DESC(), s.table.ID.DESC())
	var rows []sqliteTagRow
	if err := stmt.QueryContext(ctx, s.db, &rows); err != nil {
		return nil, fmt.Errorf("tag.sqlite.%s: %w", op, err)
	}
	out := make([]*Tag, 0, len(rows))
	for i := range rows {
		t, err := rows[i].toTag()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
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

// NOTE: SQLite reports an ON DELETE RESTRICT refusal as SQLITE_CONSTRAINT_TRIGGER (1811),
// because RESTRICT is implemented as an internal trigger; only an immediate FK failure
// (a child row naming a missing parent) reports SQLITE_CONSTRAINT_FOREIGNKEY (787).
func isSQLiteForeignKeyViolation(err error) bool {
	var sqliteErr *sqlitedrv.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY, sqlite3.SQLITE_CONSTRAINT_TRIGGER:
			return true
		}
	}
	return false
}
