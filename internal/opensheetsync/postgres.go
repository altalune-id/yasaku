package opensheetsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/go-jet/jet/v2/qrm"
	"github.com/google/uuid"

	pdb "altalune.id/yasaku/internal/platform/db"
	pgent "altalune.id/yasaku/internal/platform/db/entity/postgres"
	"altalune.id/yasaku/internal/platform/tenant"
)

// NOTE: MarkAll and MarkReferencing read the wallets, categories and transactions tables in one INSERT … SELECT, as report.Reader reads across them; a per-row port would cost one round trip per row.
type postgresStore struct {
	pc           *tenant.PgConn
	links        *pgent.OpensheetLinks
	state        *pgent.OpensheetSyncState
	wallets      *pgent.Wallets
	categories   *pgent.Categories
	transactions *pgent.Transactions
}

func newPostgresStore(pc *tenant.PgConn, schema, prefix string) *postgresStore {
	return &postgresStore{
		pc:           pc,
		links:        pgent.NewOpensheetLinks(schema, prefix),
		state:        pgent.NewOpensheetSyncState(schema, prefix),
		wallets:      pgent.NewWallets(schema, prefix),
		categories:   pgent.NewCategories(schema, prefix),
		transactions: pgent.NewTransactions(schema, prefix),
	}
}

type pgLinkRow struct {
	ID                uuid.UUID  `alias:"opensheet_links.id"`
	OrgID             uuid.UUID  `alias:"opensheet_links.org_id"`
	ProjectID         uuid.UUID  `alias:"opensheet_links.project_id"`
	OSOrg             string     `alias:"opensheet_links.os_org"`
	OSProject         string     `alias:"opensheet_links.os_project"`
	APIKeySealed      []byte     `alias:"opensheet_links.api_key_sealed"`
	APIKeyHint        string     `alias:"opensheet_links.api_key_hint"`
	TransactionsSheet string     `alias:"opensheet_links.transactions_sheet"`
	WalletsSheet      string     `alias:"opensheet_links.wallets_sheet"`
	CategoriesSheet   string     `alias:"opensheet_links.categories_sheet"`
	Enabled           bool       `alias:"opensheet_links.enabled"`
	VerifiedAt        *time.Time `alias:"opensheet_links.verified_at"`
	LastError         string     `alias:"opensheet_links.last_error"`
	LastSyncedAt      *time.Time `alias:"opensheet_links.last_synced_at"`
	FailureStreak     int32      `alias:"opensheet_links.failure_streak"`
	AutoDisabledAt    *time.Time `alias:"opensheet_links.auto_disabled_at"`
	CreatedBy         *uuid.UUID `alias:"opensheet_links.created_by"`
	CreatedByKeyID    *uuid.UUID `alias:"opensheet_links.created_by_key_id"`
	CreatedAt         time.Time  `alias:"opensheet_links.created_at"`
	UpdatedAt         time.Time  `alias:"opensheet_links.updated_at"`
}

func (r *pgLinkRow) toLink() *Link {
	return &Link{
		ID: r.ID, OrgID: r.OrgID, ProjectID: r.ProjectID, OSOrg: r.OSOrg, OSProject: r.OSProject,
		APIKeySealed: r.APIKeySealed, APIKeyHint: r.APIKeyHint,
		Sheets:  SheetSlugs{Transactions: r.TransactionsSheet, Wallets: r.WalletsSheet, Categories: r.CategoriesSheet},
		Enabled: r.Enabled, VerifiedAt: utcPtr(r.VerifiedAt), LastError: r.LastError, LastSyncedAt: utcPtr(r.LastSyncedAt),
		FailureStreak: int(r.FailureStreak), AutoDisabledAt: utcPtr(r.AutoDisabledAt),
		CreatedBy: derefUUID(r.CreatedBy), CreatedByKeyID: derefUUID(r.CreatedByKeyID),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func (s *postgresStore) txAcquire(ctx context.Context) (*sql.Tx, bool, error) {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, nil
	}
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, false, err
	}
	tx, err := s.pc.BeginTenanted(ctx, tc)
	if err != nil {
		return nil, false, fmt.Errorf("opensheetsync.postgres: begin: %w", err)
	}
	return tx, true, nil
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
		return fmt.Errorf("opensheetsync.postgres: commit: %w", cerr)
	}
	return nil
}

func (s *postgresStore) linkWhere(orgID, projectID uuid.UUID) postgres.BoolExpression {
	return s.links.OrgID.EQ(postgres.UUID(orgID)).AND(s.links.ProjectID.EQ(postgres.UUID(projectID)))
}

// NOTE: the conflict branch writes failure_streak, last_error and auto_disabled_at from the loaded link, so a SaveOutcome landing between load and save is lost; accepted: a Save follows a passing Test, Enable resets the streak and Disable turns the link off anyway.
func (s *postgresStore) SaveLink(ctx context.Context, l *Link) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t := s.links
	stmt := t.INSERT(t.AllColumns).
		VALUES(
			l.ID, l.OrgID, l.ProjectID, l.OSOrg, l.OSProject, l.APIKeySealed, l.APIKeyHint,
			l.Sheets.Transactions, l.Sheets.Wallets, l.Sheets.Categories, l.Enabled, pgTimeArg(l.VerifiedAt),
			l.LastError, pgTimeArg(l.LastSyncedAt), int64(l.FailureStreak), pgTimeArg(l.AutoDisabledAt),
			pgUUIDArg(l.CreatedBy), pgUUIDArg(l.CreatedByKeyID), l.CreatedAt.UTC(), l.UpdatedAt.UTC(),
		).
		ON_CONFLICT(t.ProjectID).
		// SECURITY: the conflict clause carries the tenant predicate; without it a Save naming another org's project rewrites that org's link.
		DO_UPDATE(postgres.SET(
			t.OSOrg.SET(postgres.String(l.OSOrg)),
			t.OSProject.SET(postgres.String(l.OSProject)),
			t.APIKeySealed.SET(postgres.Bytea(l.APIKeySealed)),
			t.APIKeyHint.SET(postgres.String(l.APIKeyHint)),
			t.TransactionsSheet.SET(postgres.String(l.Sheets.Transactions)),
			t.WalletsSheet.SET(postgres.String(l.Sheets.Wallets)),
			t.CategoriesSheet.SET(postgres.String(l.Sheets.Categories)),
			t.Enabled.SET(postgres.Bool(l.Enabled)),
			t.VerifiedAt.SET(pgTimeExpr(l.VerifiedAt)),
			t.LastError.SET(postgres.String(l.LastError)),
			t.FailureStreak.SET(postgres.Int32(int32(l.FailureStreak))), //nolint:gosec // a streak stops at DisableAfter
			t.AutoDisabledAt.SET(pgTimeExpr(l.AutoDisabledAt)),
			t.UpdatedAt.SET(postgres.TimestampzT(l.UpdatedAt.UTC())),
		).WHERE(t.OrgID.EQ(postgres.UUID(l.OrgID))))
	res, err := stmt.ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.SaveLink: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.SaveLink: rows affected: %w", err))
	}
	// NOTE: zero means the conflict-clause guard refused an upsert onto another org's link.
	if n == 0 {
		return s.endTx(tx, owned, &LinkNotFoundError{ProjectID: l.ProjectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) LinkByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Link, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	var row pgLinkRow
	err = s.links.SELECT(s.links.AllColumns).FROM(s.links).WHERE(s.linkWhere(orgID, projectID)).QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return nil, s.endTx(tx, owned, &LinkNotFoundError{ProjectID: projectID.String()})
	}
	if err != nil {
		return nil, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.LinkByProject: %w", err))
	}
	return row.toLink(), s.endTx(tx, owned, nil)
}

func (s *postgresStore) LinkEnabled(ctx context.Context, orgID, projectID uuid.UUID) (bool, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return false, err
	}
	var rows []struct {
		Enabled bool `alias:"opensheet_links.enabled"`
	}
	stmt := s.links.SELECT(s.links.Enabled).FROM(s.links).WHERE(s.linkWhere(orgID, projectID))
	// NOTE: inside a write's unit of work the share lock makes enabling (the backfill) and removing the link (the cascade) wait for the write, so a row is never missed and a state row never outlives its link.
	if !owned {
		stmt = stmt.FOR(postgres.SHARE())
	}
	err = stmt.QueryContext(ctx, tx, &rows)
	if err != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.LinkEnabled: %w", err))
	}
	return len(rows) == 1 && rows[0].Enabled, s.endTx(tx, owned, nil)
}

func (s *postgresStore) ListEnabledLinks(ctx context.Context, orgID uuid.UUID) ([]*Link, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	var rows []pgLinkRow
	err = s.links.SELECT(s.links.AllColumns).FROM(s.links).
		WHERE(s.links.OrgID.EQ(postgres.UUID(orgID)).AND(s.links.Enabled.IS_TRUE())).
		ORDER_BY(s.links.CreatedAt.ASC(), s.links.ID.ASC()).
		QueryContext(ctx, tx, &rows)
	if err != nil {
		return nil, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.ListEnabledLinks: %w", err))
	}
	out := make([]*Link, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toLink())
	}
	return out, s.endTx(tx, owned, nil)
}

func (s *postgresStore) DeleteLink(ctx context.Context, orgID, projectID uuid.UUID) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	// SECURITY: org and project predicates, not RLS alone — a BYPASSRLS role would otherwise delete another tenant's link.
	res, err := s.links.DELETE().WHERE(s.linkWhere(orgID, projectID)).ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.DeleteLink: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.DeleteLink: rows affected: %w", err))
	}
	if n == 0 {
		return s.endTx(tx, owned, &LinkNotFoundError{ProjectID: projectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *postgresStore) SaveOutcome(ctx context.Context, orgID, projectID uuid.UUID, o Outcome) (bool, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return false, err
	}
	t, at := s.links, o.At.UTC()
	// NOTE: matching the loaded updated_at leaves a link re-saved since the job loaded it alone; a stale job never penalises the new settings.
	where := s.linkWhere(orgID, projectID).AND(t.UpdatedAt.EQ(postgres.TimestampzT(o.LinkUpdatedAt.UTC())))
	if o.Err == "" {
		_, err = t.UPDATE().
			SET(t.FailureStreak.SET(postgres.Int32(0)), t.LastError.SET(postgres.String("")), t.LastSyncedAt.SET(postgres.TimestampzT(at))).
			WHERE(where).ExecContext(ctx, tx)
		if err != nil {
			return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.SaveOutcome: %w", err))
		}
		return false, s.endTx(tx, owned, nil)
	}
	var row struct {
		FailureStreak int32 `alias:"opensheet_links.failure_streak"`
	}
	err = t.UPDATE().
		SET(t.FailureStreak.SET(t.FailureStreak.ADD(postgres.Int32(1))), t.LastError.SET(postgres.String(o.Err))).
		WHERE(where).RETURNING(t.FailureStreak).QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return false, s.endTx(tx, owned, nil)
	}
	if err != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.SaveOutcome: %w", err))
	}
	if row.FailureStreak < DisableAfter {
		return false, s.endTx(tx, owned, nil)
	}
	res, err := t.UPDATE().
		SET(t.Enabled.SET(postgres.Bool(false)), t.VerifiedAt.SET(pgent.NullTimestampz()), t.AutoDisabledAt.SET(postgres.TimestampzT(at))).
		WHERE(where.AND(t.Enabled.IS_TRUE())).ExecContext(ctx, tx)
	if err != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.SaveOutcome: disable: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.postgres.SaveOutcome: rows affected: %w", err))
	}
	return n == 1, s.endTx(tx, owned, nil)
}

func pgTimeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

func pgTimeExpr(t *time.Time) postgres.TimestampzExpression {
	if t == nil {
		return pgent.NullTimestampz()
	}
	return postgres.TimestampzT(t.UTC())
}

func pgUUIDArg(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func derefUUID(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}
