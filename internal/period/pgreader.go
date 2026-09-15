package period

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
)

func (s *postgresStore) ByID(ctx context.Context, id uuid.UUID) (*Period, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	// SECURITY: org predicate, not RLS alone — a BYPASSRLS role would otherwise read another org's row.
	where := s.table.ID.EQ(postgres.UUID(id)).
		AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID)))
	return s.queryOne(ctx, tx, where, id.String())
}

func (s *postgresStore) Current(ctx context.Context, orgID, projectID uuid.UUID) (*Period, error) {
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

func (s *postgresStore) Containing(ctx context.Context, orgID, projectID uuid.UUID, d civil.Date) (*Period, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	where := s.scope(orgID, projectID, tc.OrgID).
		AND(s.table.StartDate.LT_EQ(pgDate(d))).
		AND(s.table.EndDate.IS_NULL().OR(s.table.EndDate.GT_EQ(pgDate(d))))
	return s.queryOne(ctx, tx, where, d.String())
}

func (s *postgresStore) List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Period, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	where := s.scope(orgID, projectID, tc.OrgID)
	if opts.Before != nil {
		where = where.AND(s.table.StartDate.LT(pgDate(*opts.Before)))
	}
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(where).
		ORDER_BY(s.table.StartDate.DESC(), s.table.ID.DESC())
	if opts.Limit > 0 {
		stmt = stmt.LIMIT(int64(opts.Limit))
	}
	var rows []pgPeriodRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("period.postgres.List: %w", qErr)
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

func (s *postgresStore) Neighbors(ctx context.Context, orgID, projectID, id uuid.UUID) (prev, next *Period, err error) { //nolint:nonamedreturns // mirrors the Store signature
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	scope := s.scope(orgID, projectID, tc.OrgID)
	self, err := s.queryOne(ctx, tx, scope.AND(s.table.ID.EQ(postgres.UUID(id))), id.String())
	if err != nil {
		return nil, nil, err
	}

	prev, err = s.queryEdge(ctx, tx,
		scope.AND(s.table.StartDate.LT(pgDate(self.StartDate))),
		s.table.StartDate.DESC(), s.table.ID.DESC())
	if err != nil {
		return nil, nil, err
	}
	next, err = s.queryEdge(ctx, tx,
		scope.AND(s.table.StartDate.GT(pgDate(self.StartDate))),
		s.table.StartDate.ASC(), s.table.ID.ASC())
	if err != nil {
		return nil, nil, err
	}
	return prev, next, nil
}

func (s *postgresStore) ListClosings(ctx context.Context, orgID, projectID, periodID uuid.UUID) ([]*Closing, error) {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	if owned {
		defer func() { _ = tx.Rollback() }()
	}
	stmt := postgres.SELECT(s.closings.AllColumns).
		FROM(s.closings).
		WHERE(s.closings.OrgID.EQ(postgres.UUID(orgID)).
			AND(s.closings.OrgID.EQ(postgres.UUID(tc.OrgID))).
			AND(s.closings.ProjectID.EQ(postgres.UUID(projectID))).
			AND(s.closings.PeriodID.EQ(postgres.UUID(periodID)))).
		ORDER_BY(s.closings.ClosedAt.DESC(), s.closings.ID.DESC())
	var rows []pgClosingRow
	if qErr := stmt.QueryContext(ctx, tx, &rows); qErr != nil {
		return nil, fmt.Errorf("period.postgres.ListClosings: %w", qErr)
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
func (s *postgresStore) scope(orgID, projectID, ctxOrgID uuid.UUID) postgres.BoolExpression {
	return s.table.OrgID.EQ(postgres.UUID(orgID)).
		AND(s.table.OrgID.EQ(postgres.UUID(ctxOrgID))).
		AND(s.table.ProjectID.EQ(postgres.UUID(projectID)))
}

func (s *postgresStore) queryOne(ctx context.Context, tx qrm.Queryable, where postgres.BoolExpression, notFoundID string) (*Period, error) {
	stmt := postgres.SELECT(s.table.AllColumns).FROM(s.table).WHERE(where).LIMIT(1)
	var row pgPeriodRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, &NotFoundError{ID: notFoundID}
		}
		return nil, fmt.Errorf("period.postgres.queryOne: %w", qErr)
	}
	return row.toPeriod()
}

func (s *postgresStore) queryEdge(ctx context.Context, tx qrm.Queryable, where postgres.BoolExpression, order ...postgres.OrderByClause) (*Period, error) {
	stmt := postgres.SELECT(s.table.AllColumns).
		FROM(s.table).
		WHERE(where).
		ORDER_BY(order...).
		LIMIT(1)
	var row pgPeriodRow
	if qErr := stmt.QueryContext(ctx, tx, &row); qErr != nil {
		if errors.Is(qErr, qrm.ErrNoRows) || errors.Is(qErr, sql.ErrNoRows) {
			return nil, nil //nolint:nilnil // no neighbour on this side is the normal edge case
		}
		return nil, fmt.Errorf("period.postgres.queryEdge: %w", qErr)
	}
	return row.toPeriod()
}
