package category

import (
	"context"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"

	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
)

func (s *postgresStore) Save(ctx context.Context, c *Category) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	archivedAt := pgent.NullTimestampz()
	if c.ArchivedAt != nil {
		archivedAt = postgres.TimestampzT(c.ArchivedAt.UTC())
	}
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			c.ID, c.OrgID, c.ProjectID, c.Name, string(c.Kind), c.Icon, c.Color, SortOrder32(c.SortOrder),
			archivedAt, c.CreatedAt.UTC(), c.UpdatedAt.UTC(),
		).
		ON_CONFLICT(s.table.ID).
		// SECURITY: the conflict clause carries the tenant predicate; without it an upsert carrying another org's row id rewrites that row.
		DO_UPDATE(
			postgres.SET(
				s.table.Name.SET(postgres.String(c.Name)),
				s.table.Kind.SET(postgres.String(string(c.Kind))),
				s.table.Icon.SET(postgres.String(c.Icon)),
				s.table.Color.SET(postgres.String(c.Color)),
				s.table.SortOrder.SET(postgres.Int32(SortOrder32(c.SortOrder))),
				s.table.ArchivedAt.SET(archivedAt),
				s.table.UpdatedAt.SET(postgres.TimestampzT(c.UpdatedAt.UTC())),
			).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translatePgError(execErr, c, c.ID.String()); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("category.postgres.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("category.postgres.Save: rows affected: %w", raErr))
	}
	// NOTE: zero means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: c.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: org predicate, not RLS alone — a BYPASSRLS role would otherwise delete another org's row.
	stmt := s.table.DELETE().
		WHERE(s.table.ID.EQ(postgres.UUID(id)).
			AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))))
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translatePgError(execErr, nil, id.String()); typed != nil {
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
