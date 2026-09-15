package wallet

import (
	"context"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"
)

func (s *postgresStore) Save(ctx context.Context, w *Wallet) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: the conflict clause carries the tenant predicate; without it a Save
	// holding another org's row id would take the UPDATE branch and rewrite that row.
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			w.ID, w.OrgID, w.ProjectID, w.Name, string(w.Kind), w.Provider,
			string(w.Currency), w.ExcludeFromTotal, pgNullableTime(w.ArchivedAt),
			w.CreatedAt.UTC(), w.UpdatedAt.UTC(),
		).
		ON_CONFLICT(s.table.ID).
		DO_UPDATE(
			postgres.SET(
				s.table.Name.SET(postgres.String(w.Name)),
				s.table.Kind.SET(postgres.String(string(w.Kind))),
				s.table.Provider.SET(postgres.String(w.Provider)),
				s.table.Currency.SET(postgres.String(string(w.Currency))),
				s.table.ExcludeFromTotal.SET(postgres.Bool(w.ExcludeFromTotal)),
				s.table.ArchivedAt.SET(pgNullableTimeExpr(w.ArchivedAt)),
				s.table.UpdatedAt.SET(postgres.TimestampzT(w.UpdatedAt.UTC())),
			).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))),
		)
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translatePgSaveError(execErr, w.Name); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("wallet.postgres.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("wallet.postgres.Save: rows affected: %w", raErr))
	}
	// NOTE: an insert affects one row and so does a conflicting update the caller owns; zero
	// means the conflict-clause guard refused an upsert onto a row outside the caller's org.
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: w.ID.String()})
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
		if typed := translatePgDeleteError(execErr, id.String()); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("wallet.postgres.Delete: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("wallet.postgres.Delete: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: id.String()})
	}
	return s.endTx(tx, owned, nil)
}
