package wallet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	pool  pdb.Pool
	pc    *tenant.PgConn
	table *pgent.Wallets
}

func newPostgresStore(pool pdb.Pool, pc *tenant.PgConn, schema, tablePrefix string) *postgresStore {
	return &postgresStore{pool: pool, pc: pc, table: pgent.NewWallets(schema, tablePrefix)}
}

type pgWalletRow struct {
	ID               uuid.UUID  `alias:"wallets.id"`
	OrgID            uuid.UUID  `alias:"wallets.org_id"`
	ProjectID        uuid.UUID  `alias:"wallets.project_id"`
	Name             string     `alias:"wallets.name"`
	Kind             string     `alias:"wallets.kind"`
	Provider         string     `alias:"wallets.provider"`
	Currency         string     `alias:"wallets.currency"`
	ExcludeFromTotal bool       `alias:"wallets.exclude_from_total"`
	ArchivedAt       *time.Time `alias:"wallets.archived_at"`
	CreatedAt        time.Time  `alias:"wallets.created_at"`
	UpdatedAt        time.Time  `alias:"wallets.updated_at"`
}

func (r *pgWalletRow) toWallet() *Wallet {
	w := &Wallet{
		ID:               r.ID,
		OrgID:            r.OrgID,
		ProjectID:        r.ProjectID,
		Name:             r.Name,
		Kind:             Kind(r.Kind),
		Provider:         r.Provider,
		Currency:         money.Currency(r.Currency),
		ExcludeFromTotal: r.ExcludeFromTotal,
		CreatedAt:        r.CreatedAt.UTC(),
		UpdatedAt:        r.UpdatedAt.UTC(),
	}
	if r.ArchivedAt != nil {
		t := r.ArchivedAt.UTC()
		w.ArchivedAt = &t
	}
	return w
}

// SECURITY: tenancy is resolved before the enrolled-transaction branch; a missing tenant context
// must fail rather than reach a statement whose org predicate would be the zero uuid.
func (s *postgresStore) txAcquire(ctx context.Context) (*sql.Tx, bool, tenant.Context, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, tenant.Context{}, err
	}
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, tc, nil
	}
	tx, err := s.pc.BeginTenanted(ctx, tc)
	if err != nil {
		return nil, false, tenant.Context{}, fmt.Errorf("wallet.postgres: begin: %w", err)
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
		return fmt.Errorf("wallet.postgres: commit: %w", cerr)
	}
	return nil
}

func pgNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func pgNullableTimeExpr(t *time.Time) postgres.TimestampzExpression {
	if t == nil {
		return pgent.NullTimestampz()
	}
	return postgres.TimestampzT(t.UTC())
}

func pgCode(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "", false
	}
	return pgErr.Code, true
}

func translatePgSaveError(err error, name string) error {
	if code, ok := pgCode(err); ok && code == "23505" {
		return &AlreadyExistsError{Name: name}
	}
	return nil
}

func translatePgDeleteError(err error, id string) error {
	if code, ok := pgCode(err); ok && code == "23503" {
		return &InUseError{ID: id}
	}
	return nil
}
