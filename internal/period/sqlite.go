package period

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

	"altalune.id/yasaku/civil"
	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
	"altalune.id/yasaku/internal/platform/tenant"
)

type sqliteStore struct {
	db       *sql.DB
	table    *sqliteent.Periods
	closings *sqliteent.PeriodClosings
}

func newSQLiteStore(db *sql.DB, tablePrefix string) *sqliteStore {
	return &sqliteStore{
		db:       db,
		table:    sqliteent.NewPeriods(tablePrefix),
		closings: sqliteent.NewPeriodClosings(tablePrefix),
	}
}

type sqlitePeriodRow struct {
	ID        string  `alias:"periods.id"`
	OrgID     string  `alias:"periods.org_id"`
	ProjectID string  `alias:"periods.project_id"`
	Name      string  `alias:"periods.name"`
	StartDate string  `alias:"periods.start_date"`
	EndDate   *string `alias:"periods.end_date"`
	Status    string  `alias:"periods.status"`
	ClosedAt  *string `alias:"periods.closed_at"`
	Snapshot  jsonDoc `alias:"periods.snapshot"`
	CreatedAt string  `alias:"periods.created_at"`
	UpdatedAt string  `alias:"periods.updated_at"`
}

func (r *sqlitePeriodRow) toPeriod() (*Period, error) {
	id, orgID, projectID, err := parseSQLiteIDs(r.ID, r.OrgID, r.ProjectID)
	if err != nil {
		return nil, err
	}
	start, err := civil.ParseDate(r.StartDate)
	if err != nil {
		return nil, fmt.Errorf("period.sqlite: parse start_date: %w", err)
	}
	var end *civil.Date
	if r.EndDate != nil {
		d, pErr := civil.ParseDate(*r.EndDate)
		if pErr != nil {
			return nil, fmt.Errorf("period.sqlite: parse end_date: %w", pErr)
		}
		end = &d
	}
	var closedAt *time.Time
	if r.ClosedAt != nil {
		t, pErr := time.Parse(time.RFC3339Nano, *r.ClosedAt)
		if pErr != nil {
			return nil, fmt.Errorf("period.sqlite: parse closed_at: %w", pErr)
		}
		closedAt = &t
	}
	createdAt, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("period.sqlite: parse created_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("period.sqlite: parse updated_at: %w", err)
	}
	snap, err := unmarshalSnapshot(r.Snapshot.raw)
	if err != nil {
		return nil, err
	}
	return &Period{
		ID:        id,
		OrgID:     orgID,
		ProjectID: projectID,
		Name:      r.Name,
		StartDate: start,
		EndDate:   end,
		Status:    Status(r.Status),
		ClosedAt:  closedAt,
		Snapshot:  snap,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

type sqliteClosingRow struct {
	ID        string  `alias:"period_closings.id"`
	OrgID     string  `alias:"period_closings.org_id"`
	ProjectID string  `alias:"period_closings.project_id"`
	PeriodID  string  `alias:"period_closings.period_id"`
	ClosedAt  string  `alias:"period_closings.closed_at"`
	ClosedBy  string  `alias:"period_closings.closed_by"`
	Snapshot  jsonDoc `alias:"period_closings.snapshot"`
}

func (r *sqliteClosingRow) toClosing() (*Closing, error) {
	id, orgID, projectID, err := parseSQLiteIDs(r.ID, r.OrgID, r.ProjectID)
	if err != nil {
		return nil, err
	}
	periodID, err := uuid.Parse(r.PeriodID)
	if err != nil {
		return nil, fmt.Errorf("period.sqlite: parse period_id: %w", err)
	}
	closedBy, err := uuid.Parse(r.ClosedBy)
	if err != nil {
		return nil, fmt.Errorf("period.sqlite: parse closed_by: %w", err)
	}
	closedAt, err := time.Parse(time.RFC3339Nano, r.ClosedAt)
	if err != nil {
		return nil, fmt.Errorf("period.sqlite: parse closed_at: %w", err)
	}
	snap, err := unmarshalSnapshot(r.Snapshot.raw)
	if err != nil {
		return nil, err
	}
	c := &Closing{
		ID:        id,
		OrgID:     orgID,
		ProjectID: projectID,
		PeriodID:  periodID,
		ClosedAt:  closedAt,
		ClosedBy:  closedBy,
	}
	if snap != nil {
		c.Snapshot = *snap
	}
	return c, nil
}

func parseSQLiteIDs(rawID, rawOrg, rawProject string) (id, orgID, projectID uuid.UUID, err error) { //nolint:nonamedreturns // triple of ids
	if id, err = uuid.Parse(rawID); err != nil {
		return id, orgID, projectID, fmt.Errorf("period.sqlite: parse id: %w", err)
	}
	if orgID, err = uuid.Parse(rawOrg); err != nil {
		return id, orgID, projectID, fmt.Errorf("period.sqlite: parse org_id: %w", err)
	}
	if projectID, err = uuid.Parse(rawProject); err != nil {
		return id, orgID, projectID, fmt.Errorf("period.sqlite: parse project_id: %w", err)
	}
	return id, orgID, projectID, nil
}

func sqliteText(v *string) sqlite.StringExpression {
	if v == nil {
		return sqliteent.NullText()
	}
	return sqlite.String(*v)
}

func sqliteDatePtr(d *civil.Date) sqlite.StringExpression {
	if d == nil {
		return sqliteent.NullText()
	}
	s := d.String()
	return sqlite.String(s)
}

func sqliteTimePtr(t *time.Time) sqlite.StringExpression {
	if t == nil {
		return sqliteent.NullText()
	}
	return sqlite.String(sqliteent.SQLiteTime(*t))
}

// SECURITY: the only unique index a conflicting write can reach is the partial current-period one, so a unique violation always means a second current period.
func translateSQLiteError(err error, p *Period) error {
	var sqliteErr *sqlitedrv.Error
	if !errors.As(err, &sqliteErr) {
		return nil
	}
	if sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return newOverlapError(p)
	}
	return nil
}

// txAcquire enrolls in the caller's unit of work when one is active, so a multi-statement write commits or rolls back as one.
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
		return nil, false, tenant.Context{}, fmt.Errorf("period.sqlite: begin: %w", err)
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
		return fmt.Errorf("period.sqlite: commit: %w", cerr)
	}
	return nil
}

func (s *sqliteStore) Save(ctx context.Context, p *Period) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	js, err := marshalSnapshot(p.Snapshot)
	if err != nil {
		return s.endTx(tx, owned, err)
	}
	updatedAt := sqliteent.SQLiteTime(p.UpdatedAt)
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			sqlite.String(p.ID.String()),
			sqlite.String(p.OrgID.String()),
			sqlite.String(p.ProjectID.String()),
			sqlite.String(p.Name),
			sqlite.String(p.StartDate.String()),
			sqliteDatePtr(p.EndDate),
			sqlite.String(string(p.Status)),
			sqliteTimePtr(p.ClosedAt),
			sqliteText(js),
			sqlite.String(sqliteent.SQLiteTime(p.CreatedAt)),
			sqlite.String(updatedAt),
		).
		ON_CONFLICT(s.table.ID).
		// SECURITY: SQLite has no RLS, so this tenant predicate is the only thing stopping an attacker-supplied row id from updating another org's row.
		DO_UPDATE(
			sqlite.SET(
				s.table.Name.SET(sqlite.String(p.Name)),
				s.table.StartDate.SET(sqlite.String(p.StartDate.String())),
				s.table.EndDate.SET(sqliteDatePtr(p.EndDate)),
				s.table.Status.SET(sqlite.String(string(p.Status))),
				s.table.ClosedAt.SET(sqliteTimePtr(p.ClosedAt)),
				s.table.Snapshot.SET(sqliteText(js)),
				s.table.UpdatedAt.SET(sqlite.String(updatedAt)),
			).WHERE(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String()))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translateSQLiteError(execErr, p); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("period.sqlite.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("period.sqlite.Save: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: p.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) SaveClosing(ctx context.Context, c *Closing) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: a closing may only be appended inside the caller's own scope.
	if c.OrgID != tc.OrgID || c.ProjectID != tc.ProjectID {
		return s.endTx(tx, owned, &NotFoundError{ID: c.PeriodID.String()})
	}
	js, err := marshalSnapshot(&c.Snapshot)
	if err != nil {
		return s.endTx(tx, owned, err)
	}
	stmt := s.closings.INSERT(s.closings.AllColumns).
		VALUES(
			sqlite.String(c.ID.String()),
			sqlite.String(c.OrgID.String()),
			sqlite.String(c.ProjectID.String()),
			sqlite.String(c.PeriodID.String()),
			sqlite.String(sqliteent.SQLiteTime(c.ClosedAt)),
			sqlite.String(c.ClosedBy.String()),
			sqliteText(js),
		)
	if _, execErr := stmt.ExecContext(ctx, tx); execErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("period.sqlite.SaveClosing: %w", execErr))
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) ByID(ctx context.Context, id uuid.UUID) (*Period, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	// SECURITY: SQLite has no RLS, so this org predicate is the only scope guard on the read.
	where := s.table.ID.EQ(sqlite.String(id.String())).
		AND(s.table.OrgID.EQ(sqlite.String(tc.OrgID.String())))
	return s.queryOne(ctx, tx, where, id.String())
}

func (s *sqliteStore) Current(ctx context.Context, orgID, projectID uuid.UUID) (*Period, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	where := s.scope(orgID, projectID, tc.OrgID).AND(s.table.EndDate.IS_NULL())
	return s.queryOne(ctx, tx, where, projectID.String())
}

// NOTE: dates are stored as ISO YYYY-MM-DD, so SQLite's string comparison is chronological.
func (s *sqliteStore) Containing(ctx context.Context, orgID, projectID uuid.UUID, d civil.Date) (*Period, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	iso := sqlite.String(d.String())
	where := s.scope(orgID, projectID, tc.OrgID).
		AND(s.table.StartDate.LT_EQ(iso)).
		AND(s.table.EndDate.IS_NULL().OR(s.table.EndDate.GT_EQ(iso)))
	return s.queryOne(ctx, tx, where, d.String())
}

func (s *sqliteStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Period, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	where := s.scope(orgID, projectID, tc.OrgID)
	if opts.Before != nil {
		where = where.AND(s.table.StartDate.LT(sqlite.String(opts.Before.String())))
	}
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(where).
		ORDER_BY(s.table.StartDate.DESC(), s.table.ID.DESC())
	if opts.Limit > 0 {
		stmt = stmt.LIMIT(int64(opts.Limit))
	}
	var rows []sqlitePeriodRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("period.sqlite.List: %w", qErr)
	}
	out := make([]*Period, 0, len(rows))
	for i := range rows {
		p, cErr := rows[i].toPeriod()
		if cErr != nil {
			return nil, cErr
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *sqliteStore) Neighbors(ctx context.Context, orgID, projectID, id uuid.UUID) (prev, next *Period, err error) { //nolint:nonamedreturns // mirrors the Store signature
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	scope := s.scope(orgID, projectID, tc.OrgID)
	self, err := s.queryOne(ctx, tx, scope.AND(s.table.ID.EQ(sqlite.String(id.String()))), id.String())
	if err != nil {
		return nil, nil, err
	}
	start := sqlite.String(self.StartDate.String())
	prev, err = s.queryEdge(ctx, tx, scope.AND(s.table.StartDate.LT(start)), s.table.StartDate.DESC(), s.table.ID.DESC())
	if err != nil {
		return nil, nil, err
	}
	next, err = s.queryEdge(ctx, tx, scope.AND(s.table.StartDate.GT(start)), s.table.StartDate.ASC(), s.table.ID.ASC())
	if err != nil {
		return nil, nil, err
	}
	return prev, next, nil
}

func (s *sqliteStore) ListClosings(ctx context.Context, orgID, projectID, periodID uuid.UUID) ([]*Closing, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := sqlite.SELECT(s.closings.AllColumns).
		FROM(s.closings).
		WHERE(s.closings.OrgID.EQ(sqlite.String(orgID.String())).
			AND(s.closings.OrgID.EQ(sqlite.String(tc.OrgID.String()))).
			AND(s.closings.ProjectID.EQ(sqlite.String(projectID.String()))).
			AND(s.closings.PeriodID.EQ(sqlite.String(periodID.String())))).
		ORDER_BY(s.closings.ClosedAt.DESC(), s.closings.ID.DESC())
	var rows []sqliteClosingRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("period.sqlite.ListClosings: %w", qErr)
	}
	out := make([]*Closing, 0, len(rows))
	for i := range rows {
		c, cErr := rows[i].toClosing()
		if cErr != nil {
			return nil, cErr
		}
		out = append(out, c)
	}
	return out, nil
}

// SECURITY: both the caller's org and the request's tenant org are asserted, so a mismatched argument matches no rows.
func (s *sqliteStore) scope(orgID, projectID, ctxOrgID uuid.UUID) sqlite.BoolExpression {
	return s.table.OrgID.EQ(sqlite.String(orgID.String())).
		AND(s.table.OrgID.EQ(sqlite.String(ctxOrgID.String()))).
		AND(s.table.ProjectID.EQ(sqlite.String(projectID.String())))
}

func (s *sqliteStore) queryOne(ctx context.Context, tx qrm.Queryable, where sqlite.BoolExpression, notFoundID string) (*Period, error) {
	stmt := sqlite.SELECT(s.table.AllColumns).FROM(s.table).WHERE(where).LIMIT(1)
	var row sqlitePeriodRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: notFoundID}
		}
		return nil, fmt.Errorf("period.sqlite.queryOne: %w", qErr)
	}
	return row.toPeriod()
}

func (s *sqliteStore) queryEdge(ctx context.Context, tx qrm.Queryable, where sqlite.BoolExpression, order ...sqlite.OrderByClause) (*Period, error) {
	stmt := sqlite.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(where).
		ORDER_BY(order...).
		LIMIT(1)
	var row sqlitePeriodRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, nil //nolint:nilnil // no neighbour on this side is the normal edge case
		}
		return nil, fmt.Errorf("period.sqlite.queryEdge: %w", qErr)
	}
	return row.toPeriod()
}
