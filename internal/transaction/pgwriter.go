package transaction

import (
	"context"
	"fmt"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"

	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

func (s *postgresStore) Save(ctx context.Context, t *Transaction) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	noteNorm := NormalizeNote(t.Note)
	stmt := s.table.INSERT(s.table.AllColumns).
		VALUES(
			t.ID, t.OrgID, t.ProjectID, t.WalletID, pgUUIDArg(t.ToWalletID),
			string(t.Kind), t.Amount.Minor, string(t.Amount.Currency),
			pgUUIDArg(t.CategoryID), pgUUIDArg(t.PeriodID),
			t.Note, noteNorm, t.OccurredAt.UTC(), t.CreatedBy,
			t.CreatedAt.UTC(), t.UpdatedAt.UTC(),
		).
		ON_CONFLICT(s.table.ID).
		// SECURITY: the conflict clause carries the tenant predicate; without it an attacker-supplied row id rewrites another org's row.
		DO_UPDATE(
			postgres.SET(
				s.table.WalletID.SET(postgres.UUID(t.WalletID)),
				s.table.ToWalletID.SET(pgUUIDExpr(t.ToWalletID)),
				s.table.Kind.SET(postgres.String(string(t.Kind))),
				s.table.AmountMinor.SET(postgres.Int64(t.Amount.Minor)),
				s.table.Currency.SET(postgres.String(string(t.Amount.Currency))),
				s.table.CategoryID.SET(pgUUIDExpr(t.CategoryID)),
				s.table.PeriodID.SET(pgUUIDExpr(t.PeriodID)),
				s.table.Note.SET(postgres.String(t.Note)),
				s.table.NoteNorm.SET(postgres.String(noteNorm)),
				s.table.OccurredAt.SET(postgres.TimestampzT(t.OccurredAt.UTC())),
				s.table.UpdatedAt.SET(postgres.TimestampzT(t.UpdatedAt.UTC())),
			).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))),
		)

	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		if typed := translatePgConstraint(execErr, t); typed != nil {
			return s.endTx(tx, owned, typed)
		}
		return s.endTx(tx, owned, fmt.Errorf("transaction.postgres.Save: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("transaction.postgres.Save: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: t.ID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) Delete(ctx context.Context, id uuid.UUID) error {
	tx, owned, tc, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: org predicate, not RLS alone — a BYPASSRLS role would otherwise delete another org's row.
	stmt := s.table.DELETE().WHERE(s.table.ID.EQ(postgres.UUID(id)).
		AND(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))))
	res, execErr := stmt.ExecContext(ctx, tx)
	if execErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("transaction.postgres.Delete: %w", execErr))
	}
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return s.endTx(tx, owned, fmt.Errorf("transaction.postgres.Delete: rows affected: %w", raErr))
	}
	if n == 0 {
		return s.endTx(tx, owned, &NotFoundError{ID: id.String()})
	}
	return s.endTx(tx, owned, nil)
}

// SECURITY: an advisory lock outside a transaction is released at once, so a caller that forgot its unit of work must fail loudly rather than race silently.
func (s *postgresStore) LockWallet(ctx context.Context, orgID, projectID, walletID uuid.UUID) error {
	tx, ok := pdb.CurrentTx(ctx)
	if !ok {
		return fmt.Errorf("transaction.postgres.LockWallet: %w", ErrNoUnitOfWork)
	}
	if _, err := tenant.From(ctx); err != nil {
		return err
	}
	// NOTE: Exec, not Query — pg_advisory_xact_lock returns void, which has no column to scan.
	stmt := postgres.RawStatement(
		`SELECT pg_advisory_xact_lock(#class, #key)`,
		postgres.RawArgs{
			"#class": walletLockClass,
			"#key":   advisoryKey(orgID, projectID, walletID),
		},
	)
	if _, err := stmt.ExecContext(ctx, tx); err != nil {
		return fmt.Errorf("transaction.postgres.LockWallet: %w", err)
	}
	return nil
}
