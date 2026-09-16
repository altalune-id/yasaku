package transaction

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	pdb "altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

type postgresStore struct {
	pool      pdb.Pool
	pc        *tenant.PgConn
	table     *pgent.Transactions
	qualified string
}

func newPostgresStore(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresStore {
	if schema == "" {
		schema = "public"
	}
	return &postgresStore{
		pool:      pool,
		pc:        pc,
		table:     pgent.NewTransactions(schema, tablePrefix),
		qualified: schema + "." + tablePrefix + "transactions",
	}
}

type pgTxnRow struct {
	ID          uuid.UUID  `alias:"transactions.id"`
	OrgID       uuid.UUID  `alias:"transactions.org_id"`
	ProjectID   uuid.UUID  `alias:"transactions.project_id"`
	WalletID    uuid.UUID  `alias:"transactions.wallet_id"`
	ToWalletID  *uuid.UUID `alias:"transactions.to_wallet_id"`
	Kind        string     `alias:"transactions.kind"`
	AmountMinor int64      `alias:"transactions.amount_minor"`
	Currency    string     `alias:"transactions.currency"`
	CategoryID  *uuid.UUID `alias:"transactions.category_id"`
	PeriodID    *uuid.UUID `alias:"transactions.period_id"`
	Note        string     `alias:"transactions.note"`
	NoteNorm    string     `alias:"transactions.note_norm"`
	OccurredAt  time.Time  `alias:"transactions.occurred_at"`
	CreatedBy   uuid.UUID  `alias:"transactions.created_by"`
	CreatedAt   time.Time  `alias:"transactions.created_at"`
	UpdatedAt   time.Time  `alias:"transactions.updated_at"`
}

func (r *pgTxnRow) toTransaction() *Transaction {
	return &Transaction{
		ID:         r.ID,
		OrgID:      r.OrgID,
		ProjectID:  r.ProjectID,
		WalletID:   r.WalletID,
		ToWalletID: r.ToWalletID,
		Kind:       Kind(r.Kind),
		Amount:     money.New(r.AmountMinor, money.Currency(r.Currency)),
		CategoryID: r.CategoryID,
		PeriodID:   r.PeriodID,
		Note:       r.Note,
		OccurredAt: r.OccurredAt.UTC(),
		CreatedBy:  r.CreatedBy,
		CreatedAt:  r.CreatedAt.UTC(),
		UpdatedAt:  r.UpdatedAt.UTC(),
	}
}

type pgBalanceRow struct {
	WalletID uuid.UUID `alias:"balances.wallet_id"`
	Currency string    `alias:"balances.currency"`
	Total    int64     `alias:"balances.total"`
}

func (s *postgresStore) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		tc, err := tenant.From(ctx)
		if err != nil {
			return nil, false, tenant.Context{}, err
		}
		return tx, false, tc, nil
	}
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	tx, err := s.pc.BeginTenanted(ctx, tc)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("transaction.postgres: begin: %w", err)
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
		return fmt.Errorf("transaction.postgres: commit: %w", cerr)
	}
	return nil
}

func pgUUIDArg(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

func pgUUIDExpr(id *uuid.UUID) postgres.StringExpression {
	if id == nil {
		return pgent.NullUUID()
	}
	return postgres.UUID(*id)
}

func translatePgConstraint(err error, t *Transaction) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		return nil
	}
	switch {
	case strings.Contains(pgErr.ConstraintName, "to_wallet"):
		return &NotFoundError{ID: uuidString(t.ToWalletID)}
	case strings.Contains(pgErr.ConstraintName, "wallet"):
		return &NotFoundError{ID: t.WalletID.String()}
	case strings.Contains(pgErr.ConstraintName, "category"):
		return &NotFoundError{ID: uuidString(t.CategoryID)}
	case strings.Contains(pgErr.ConstraintName, "period"):
		return &NotFoundError{ID: uuidString(t.PeriodID)}
	}
	return nil
}

func uuidString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// NOTE: the two-argument advisory space is disjoint from the single-argument one db.LockKey uses for scheduler jobs, whose holder would otherwise see pg_try_advisory_lock fail and silently skip a run.
const walletLockClass int32 = 0x7961736B

// NOTE: a collision within this class only over-serializes two wallets, never affecting correctness.
func advisoryKey(orgID, projectID, walletID uuid.UUID) int32 {
	var key uint64
	for _, id := range [3]uuid.UUID{orgID, projectID, walletID} {
		hi := binary.BigEndian.Uint64(id[0:8])
		lo := binary.BigEndian.Uint64(id[8:16])
		key = key*1099511628211 ^ hi
		key = key*1099511628211 ^ lo
	}
	return int32(uint32(key ^ (key >> 32))) //nolint:gosec // a lock key is an opaque bit pattern, not a magnitude.
}
