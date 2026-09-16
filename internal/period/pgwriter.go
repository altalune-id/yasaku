package period

import (
	"context"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
)

func (s *postgresStore) Save(ctx context.Context, p *Period) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	js, err := marshalSnapshot(p.Snapshot)
	if err != nil {
		return s.endTx(tx, owned, err)
	}
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			postgres.UUID(p.ID),
			postgres.UUID(p.OrgID),
			postgres.UUID(p.ProjectID),
			postgres.String(p.Name),
			pgDate(p.StartDate),
			pgDatePtr(p.EndDate),
			postgres.String(string(p.Status)),
			pgTimePtr(p.ClosedAt),
			pgJSON(js),
			postgres.TimestampzT(p.CreatedAt.UTC()),
			postgres.TimestampzT(p.UpdatedAt.UTC()),
		).
		ON_CONFLICT(s.table.ID).
		// SECURITY: the conflict clause carries the tenant predicate; without it an attacker-supplied row id updates another org's row.
		DO_UPDATE(
			postgres.SET(
				s.table.Name.SET(postgres.String(p.Name)),
				s.table.StartDate.SET(pgDate(p.StartDate)),
				s.table.EndDate.SET(pgDatePtr(p.EndDate)),
				s.table.Status.SET(postgres.String(string(p.Status))),
				s.table.ClosedAt.SET(pgTimePtr(p.ClosedAt)),
				s.table.Snapshot.SET(pgJSON(js)),
				s.table.UpdatedAt.SET(postgres.TimestampzT(p.UpdatedAt.UTC())),
			).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translatePgError(execErr, p); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("period.postgres.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("period.postgres.Save: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: p.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) SaveClosing(ctx context.Context, c *Closing) error {
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
			postgres.UUID(c.ID),
			postgres.UUID(c.OrgID),
			postgres.UUID(c.ProjectID),
			postgres.UUID(c.PeriodID),
			postgres.TimestampzT(c.ClosedAt.UTC()),
			postgres.UUID(c.ClosedBy),
			pgJSON(js),
		)
	if _, execErr := stmt.ExecContext(ctx, tx); execErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("period.postgres.SaveClosing: %w", execErr))
	}
	return s.endTx(tx, owned, nil)
}
