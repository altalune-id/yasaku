package opensheetsync

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"

	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
)

// NOTE: SQLite has no RLS, so the org predicate on every statement here is the only tenant guard.
// NOTE: a transaction this store opens itself is DEFERRED, so every method that writes writes in its first statement (UPDATE … RETURNING, never a SELECT first); a read before the write would fail with SQLITE_BUSY_SNAPSHOT when another writer commits in between.
type sqliteStore struct {
	db           *sql.DB
	links        *sqliteent.OpensheetLinks
	state        *sqliteent.OpensheetSyncState
	wallets      *sqliteent.Wallets
	categories   *sqliteent.Categories
	transactions *sqliteent.Transactions
}

func newSQLiteStore(db *sql.DB, prefix string) *sqliteStore {
	return &sqliteStore{
		db:           db,
		links:        sqliteent.NewOpensheetLinks(prefix),
		state:        sqliteent.NewOpensheetSyncState(prefix),
		wallets:      sqliteent.NewWallets(prefix),
		categories:   sqliteent.NewCategories(prefix),
		transactions: sqliteent.NewTransactions(prefix),
	}
}

type sqliteLinkRow struct {
	ID                string  `alias:"opensheet_links.id"`
	OrgID             string  `alias:"opensheet_links.org_id"`
	ProjectID         string  `alias:"opensheet_links.project_id"`
	OSOrg             string  `alias:"opensheet_links.os_org"`
	OSProject         string  `alias:"opensheet_links.os_project"`
	APIKeySealed      []byte  `alias:"opensheet_links.api_key_sealed"`
	APIKeyHint        string  `alias:"opensheet_links.api_key_hint"`
	TransactionsSheet string  `alias:"opensheet_links.transactions_sheet"`
	WalletsSheet      string  `alias:"opensheet_links.wallets_sheet"`
	CategoriesSheet   string  `alias:"opensheet_links.categories_sheet"`
	Enabled           int64   `alias:"opensheet_links.enabled"`
	VerifiedAt        *string `alias:"opensheet_links.verified_at"`
	LastError         string  `alias:"opensheet_links.last_error"`
	LastSyncedAt      *string `alias:"opensheet_links.last_synced_at"`
	FailureStreak     int64   `alias:"opensheet_links.failure_streak"`
	AutoDisabledAt    *string `alias:"opensheet_links.auto_disabled_at"`
	CreatedBy         *string `alias:"opensheet_links.created_by"`
	CreatedByKeyID    *string `alias:"opensheet_links.created_by_key_id"`
	CreatedAt         string  `alias:"opensheet_links.created_at"`
	UpdatedAt         string  `alias:"opensheet_links.updated_at"`
}

func (r *sqliteLinkRow) toLink() (*Link, error) {
	var err error
	l := &Link{
		OSOrg: r.OSOrg, OSProject: r.OSProject, APIKeySealed: r.APIKeySealed, APIKeyHint: r.APIKeyHint,
		Sheets:  SheetSlugs{Transactions: r.TransactionsSheet, Wallets: r.WalletsSheet, Categories: r.CategoriesSheet},
		Enabled: r.Enabled != 0, LastError: r.LastError, FailureStreak: int(r.FailureStreak),
	}
	ids := []struct {
		dst *uuid.UUID
		src *string
	}{{&l.ID, &r.ID}, {&l.OrgID, &r.OrgID}, {&l.ProjectID, &r.ProjectID}, {&l.CreatedBy, r.CreatedBy}, {&l.CreatedByKeyID, r.CreatedByKeyID}}
	for _, f := range ids {
		if *f.dst, err = sqliteUUID(f.src); err != nil {
			return nil, err
		}
	}
	times := []struct {
		dst **time.Time
		src *string
	}{{&l.VerifiedAt, r.VerifiedAt}, {&l.LastSyncedAt, r.LastSyncedAt}, {&l.AutoDisabledAt, r.AutoDisabledAt}}
	for _, f := range times {
		if *f.dst, err = sqliteTimePtr(f.src); err != nil {
			return nil, err
		}
	}
	if l.CreatedAt, err = sqliteTime(r.CreatedAt); err != nil {
		return nil, err
	}
	if l.UpdatedAt, err = sqliteTime(r.UpdatedAt); err != nil {
		return nil, err
	}
	return l, nil
}

type sqliteStateRow struct {
	ProjectID     string `alias:"opensheet_sync_state.project_id"`
	Entity        string `alias:"opensheet_sync_state.entity"`
	EntityID      string `alias:"opensheet_sync_state.entity_id"`
	Version       int64  `alias:"opensheet_sync_state.version"`
	SyncedVersion int64  `alias:"opensheet_sync_state.synced_version"`
	Deleted       int64  `alias:"opensheet_sync_state.deleted"`
	Attempts      int64  `alias:"opensheet_sync_state.attempts"`
	LastError     string `alias:"opensheet_sync_state.last_error"`
	UpdatedAt     string `alias:"opensheet_sync_state.updated_at"`
}

func (r *sqliteStateRow) toState() (State, error) {
	pid, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return State{}, fmt.Errorf("opensheetsync.sqlite: parse project_id: %w", err)
	}
	eid, err := uuid.Parse(r.EntityID)
	if err != nil {
		return State{}, fmt.Errorf("opensheetsync.sqlite: parse entity_id: %w", err)
	}
	at, err := sqliteTime(r.UpdatedAt)
	if err != nil {
		return State{}, err
	}
	return State{
		Ref:       Ref{Entity: Entity(r.Entity), ID: eid, Deleted: r.Deleted != 0},
		ProjectID: pid, Version: r.Version, SyncedVersion: r.SyncedVersion,
		Attempts: int(r.Attempts), LastError: r.LastError, UpdatedAt: at,
	}, nil
}

func (s *sqliteStore) txAcquire(ctx context.Context) (*sql.Tx, bool, error) {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx, false, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("opensheetsync.sqlite: begin: %w", err)
	}
	return tx, true, nil
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
		return fmt.Errorf("opensheetsync.sqlite: commit: %w", cerr)
	}
	return nil
}

func (s *sqliteStore) linkWhere(orgID, projectID uuid.UUID) sqlite.BoolExpression {
	return s.links.OrgID.EQ(sqlite.String(orgID.String())).AND(s.links.ProjectID.EQ(sqlite.String(projectID.String())))
}

// NOTE: the conflict branch writes failure_streak, last_error and auto_disabled_at from the loaded link, so a SaveOutcome landing between load and save is lost; accepted: a Save follows a passing Test, Enable resets the streak and Disable turns the link off anyway.
func (s *sqliteStore) SaveLink(ctx context.Context, l *Link) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t := s.links
	stmt := t.INSERT(t.AllColumns).
		VALUES(
			l.ID.String(), l.OrgID.String(), l.ProjectID.String(), l.OSOrg, l.OSProject, l.APIKeySealed, l.APIKeyHint,
			l.Sheets.Transactions, l.Sheets.Wallets, l.Sheets.Categories, boolInt(l.Enabled), sqliteTimeArg(l.VerifiedAt),
			l.LastError, sqliteTimeArg(l.LastSyncedAt), int64(l.FailureStreak), sqliteTimeArg(l.AutoDisabledAt),
			sqliteUUIDArg(l.CreatedBy), sqliteUUIDArg(l.CreatedByKeyID),
			sqliteent.SQLiteTime(l.CreatedAt), sqliteent.SQLiteTime(l.UpdatedAt),
		).
		ON_CONFLICT(t.ProjectID).
		// SECURITY: SQLite has no RLS, so this tenant predicate is the only thing stopping a Save from rewriting another org's link.
		DO_UPDATE(sqlite.SET(
			t.OSOrg.SET(sqlite.String(l.OSOrg)),
			t.OSProject.SET(sqlite.String(l.OSProject)),
			t.APIKeySealed.SET(sqlite.Blob(l.APIKeySealed)),
			t.APIKeyHint.SET(sqlite.String(l.APIKeyHint)),
			t.TransactionsSheet.SET(sqlite.String(l.Sheets.Transactions)),
			t.WalletsSheet.SET(sqlite.String(l.Sheets.Wallets)),
			t.CategoriesSheet.SET(sqlite.String(l.Sheets.Categories)),
			t.Enabled.SET(sqlite.Int(boolInt(l.Enabled))),
			t.VerifiedAt.SET(sqliteTimeExpr(l.VerifiedAt)),
			t.LastError.SET(sqlite.String(l.LastError)),
			t.FailureStreak.SET(sqlite.Int(int64(l.FailureStreak))),
			t.AutoDisabledAt.SET(sqliteTimeExpr(l.AutoDisabledAt)),
			t.UpdatedAt.SET(sqlite.String(sqliteent.SQLiteTime(l.UpdatedAt))),
		).WHERE(t.OrgID.EQ(sqlite.String(l.OrgID.String()))))
	res, err := stmt.ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.SaveLink: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.SaveLink: rows affected: %w", err))
	}
	// NOTE: zero means the conflict-clause guard refused an upsert onto another org's link.
	if n == 0 {
		return s.endTx(tx, owned, &LinkNotFoundError{ProjectID: l.ProjectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) LinkByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Link, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	var row sqliteLinkRow
	err = s.links.SELECT(s.links.AllColumns).FROM(s.links).WHERE(s.linkWhere(orgID, projectID)).QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return nil, s.endTx(tx, owned, &LinkNotFoundError{ProjectID: projectID.String()})
	}
	if err != nil {
		return nil, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.LinkByProject: %w", err))
	}
	l, err := row.toLink()
	if err != nil {
		return nil, s.endTx(tx, owned, err)
	}
	return l, s.endTx(tx, owned, nil)
}

// NOTE: no lock: a write's unit of work begins IMMEDIATE and holds the single writer lock, so enabling or removing the link already waits for it.
func (s *sqliteStore) LinkEnabled(ctx context.Context, orgID, projectID uuid.UUID) (bool, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return false, err
	}
	var rows []struct {
		Enabled int64 `alias:"opensheet_links.enabled"`
	}
	err = s.links.SELECT(s.links.Enabled).FROM(s.links).WHERE(s.linkWhere(orgID, projectID)).QueryContext(ctx, tx, &rows)
	if err != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.LinkEnabled: %w", err))
	}
	return len(rows) == 1 && rows[0].Enabled != 0, s.endTx(tx, owned, nil)
}

func (s *sqliteStore) ListEnabledLinks(ctx context.Context, orgID uuid.UUID) ([]*Link, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	var rows []sqliteLinkRow
	err = s.links.SELECT(s.links.AllColumns).FROM(s.links).
		WHERE(s.links.OrgID.EQ(sqlite.String(orgID.String())).AND(s.links.Enabled.EQ(sqlite.Int(1)))).
		ORDER_BY(s.links.CreatedAt.ASC(), s.links.ID.ASC()).
		QueryContext(ctx, tx, &rows)
	if err != nil {
		return nil, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.ListEnabledLinks: %w", err))
	}
	out := make([]*Link, 0, len(rows))
	for i := range rows {
		l, cErr := rows[i].toLink()
		if cErr != nil {
			return nil, s.endTx(tx, owned, cErr)
		}
		out = append(out, l)
	}
	return out, s.endTx(tx, owned, nil)
}

func (s *sqliteStore) DeleteLink(ctx context.Context, orgID, projectID uuid.UUID) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	res, err := s.links.DELETE().WHERE(s.linkWhere(orgID, projectID)).ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.DeleteLink: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.DeleteLink: rows affected: %w", err))
	}
	if n == 0 {
		return s.endTx(tx, owned, &LinkNotFoundError{ProjectID: projectID.String()})
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) SaveOutcome(ctx context.Context, orgID, projectID uuid.UUID, o Outcome) (bool, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return false, err
	}
	t, at := s.links, sqliteent.SQLiteTime(o.At)
	// NOTE: matching the loaded updated_at leaves a link re-saved since the job loaded it alone; a stale job never penalises the new settings.
	where := s.linkWhere(orgID, projectID).AND(t.UpdatedAt.EQ(sqlite.String(sqliteent.SQLiteTime(o.LinkUpdatedAt))))
	if o.Err == "" {
		_, err = t.UPDATE().
			SET(t.FailureStreak.SET(sqlite.Int(0)), t.LastError.SET(sqlite.String("")), t.LastSyncedAt.SET(sqlite.String(at))).
			WHERE(where).ExecContext(ctx, tx)
		if err != nil {
			return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.SaveOutcome: %w", err))
		}
		return false, s.endTx(tx, owned, nil)
	}
	var row struct {
		FailureStreak int64 `alias:"opensheet_links.failure_streak"`
	}
	err = t.UPDATE().
		SET(t.FailureStreak.SET(t.FailureStreak.ADD(sqlite.Int(1))), t.LastError.SET(sqlite.String(o.Err))).
		WHERE(where).RETURNING(sqlite.IntegerColumn("failure_streak").AS("opensheet_links.failure_streak")).QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return false, s.endTx(tx, owned, nil)
	}
	if err != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.SaveOutcome: %w", err))
	}
	if row.FailureStreak < DisableAfter {
		return false, s.endTx(tx, owned, nil)
	}
	res, err := t.UPDATE().
		SET(t.Enabled.SET(sqlite.Int(0)), t.VerifiedAt.SET(sqliteent.NullText()), t.AutoDisabledAt.SET(sqlite.String(at))).
		WHERE(where.AND(t.Enabled.EQ(sqlite.Int(1)))).ExecContext(ctx, tx)
	if err != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.SaveOutcome: disable: %w", err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.SaveOutcome: rows affected: %w", err))
	}
	return n == 1, s.endTx(tx, owned, nil)
}

func (s *sqliteStore) stateKey(orgID, projectID uuid.UUID, ref Ref) sqlite.BoolExpression {
	t := s.state
	return t.OrgID.EQ(sqlite.String(orgID.String())).
		AND(t.ProjectID.EQ(sqlite.String(projectID.String()))).
		AND(t.Entity.EQ(sqlite.String(string(ref.Entity)))).
		AND(t.EntityID.EQ(sqlite.String(ref.ID.String())))
}

func (s *sqliteStore) markColumns() sqlite.ColumnList {
	t := s.state
	return sqlite.ColumnList{t.OrgID, t.ProjectID, t.Entity, t.EntityID, t.Version, t.SyncedVersion, t.Deleted, t.Attempts, t.LastError, t.UpdatedAt}
}

// NOTE: a mark gives the row fresh attempts and drops its retry_after, so a new edit is tried again at once.
func (s *sqliteStore) markSets(ts string, deleted bool) []sqlite.ColumnAssigment {
	t := s.state
	sets := []sqlite.ColumnAssigment{
		t.Version.SET(t.Version.ADD(sqlite.Int(1))),
		t.Attempts.SET(sqlite.Int(0)),
		t.RetryAfter.SET(sqliteent.NullText()),
		t.UpdatedAt.SET(sqlite.String(ts)),
	}
	if deleted {
		sets = append(sets, t.Deleted.SET(sqlite.Int(1)))
	}
	return sets
}

func (s *sqliteStore) Mark(ctx context.Context, orgID, projectID uuid.UUID, refs []Ref, at time.Time) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t, ts := s.state, sqliteent.SQLiteTime(at)
	for _, r := range LockOrder(refs) {
		stmt := t.INSERT(s.markColumns()).
			VALUES(orgID.String(), projectID.String(), string(r.Entity), r.ID.String(), 1, 0, boolInt(r.Deleted), 0, "", ts).
			ON_CONFLICT(t.OrgID, t.Entity, t.EntityID).
			// SECURITY: SQLite has no RLS, so this tenant predicate is the only guard on the conflict branch.
			DO_UPDATE(sqlite.SET(s.markSets(ts, r.Deleted)...).WHERE(t.OrgID.EQ(sqlite.String(orgID.String()))))
		if _, err := stmt.ExecContext(ctx, tx); err != nil {
			return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.Mark: %w", err))
		}
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) markSelect(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, ts string, sel sqlite.SelectStatement) (int64, error) {
	t := s.state
	res, err := t.INSERT(s.markColumns()).
		QUERY(sel).
		ON_CONFLICT(t.OrgID, t.Entity, t.EntityID).
		// SECURITY: SQLite has no RLS, so this tenant predicate is the only guard on the conflict branch.
		DO_UPDATE(sqlite.SET(s.markSets(ts, false)...).WHERE(t.OrgID.EQ(sqlite.String(orgID.String())))).
		ExecContext(ctx, tx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *sqliteStore) markRow(e Entity, ts string, id, org, project sqlite.Column) sqlite.SelectStatement {
	return sqlite.SELECT(org, project, sqlite.String(string(e)), id,
		sqlite.Int(1), sqlite.Int(0), sqlite.Int(0), sqlite.Int(0), sqlite.String(""), sqlite.String(ts))
}

func (s *sqliteStore) MarkReferencing(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, at time.Time) (int64, error) {
	txs, id := s.transactions, sqlite.String(ref.ID.String())
	var cond sqlite.BoolExpression
	switch ref.Entity {
	case EntityWallet:
		cond = txs.WalletID.EQ(id).OR(txs.ToWalletID.EQ(id))
	case EntityCategory:
		cond = txs.CategoryID.EQ(id)
	default:
		return 0, nil
	}
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return 0, err
	}
	ts := sqliteent.SQLiteTime(at)
	sel := s.markRow(EntityTransaction, ts, txs.ID, txs.OrgID, txs.ProjectID).FROM(txs).
		WHERE(txs.OrgID.EQ(sqlite.String(orgID.String())).AND(txs.ProjectID.EQ(sqlite.String(projectID.String()))).AND(cond)).
		ORDER_BY(txs.ID.ASC())
	n, err := s.markSelect(ctx, tx, orgID, ts, sel)
	if err != nil {
		return 0, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.MarkReferencing: %w", err))
	}
	return n, s.endTx(tx, owned, nil)
}

func (s *sqliteStore) MarkAll(ctx context.Context, orgID, projectID uuid.UUID, at time.Time) (int64, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return 0, err
	}
	ts, org, project := sqliteent.SQLiteTime(at), sqlite.String(orgID.String()), sqlite.String(projectID.String())
	w, c, txs := s.wallets, s.categories, s.transactions
	sources := []struct {
		entity  Entity
		table   sqlite.ReadableTable
		id, org sqlite.ColumnString
		project sqlite.ColumnString
	}{
		{EntityTransaction, txs, txs.ID, txs.OrgID, txs.ProjectID},
		{EntityCategory, c, c.ID, c.OrgID, c.ProjectID},
		{EntityWallet, w, w.ID, w.OrgID, w.ProjectID},
	}
	var total int64
	for _, src := range sources {
		sel := s.markRow(src.entity, ts, src.id, src.org, src.project).FROM(src.table).
			WHERE(src.org.EQ(org).AND(src.project.EQ(project))).ORDER_BY(src.id.ASC())
		n, err := s.markSelect(ctx, tx, orgID, ts, sel)
		if err != nil {
			return 0, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.MarkAll: %w", err))
		}
		gone, err := s.markGone(ctx, tx, orgID, projectID, src.entity, ts, src.table, src.id, src.org)
		if err != nil {
			return 0, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.MarkAll: gone: %w", err))
		}
		total += n + gone
	}
	return total, s.endTx(tx, owned, nil)
}

// NOTE: every domain row is hard-deleted, so a state row whose entity no longer exists is a delete made while the link was off; marking it deleted lets the backfill tombstone it in the sheet.
func (s *sqliteStore) markGone(ctx context.Context, tx *sql.Tx, orgID, projectID uuid.UUID, e Entity, ts string, table sqlite.ReadableTable, id, org sqlite.ColumnString) (int64, error) {
	t, orgArg := s.state, sqlite.String(orgID.String())
	res, err := t.UPDATE().
		SET(
			t.Version.SET(t.Version.ADD(sqlite.Int(1))),
			t.Deleted.SET(sqlite.Int(1)),
			t.Attempts.SET(sqlite.Int(0)),
			t.RetryAfter.SET(sqliteent.NullText()),
			t.UpdatedAt.SET(sqlite.String(ts)),
		).
		WHERE(t.OrgID.EQ(orgArg).
			AND(t.ProjectID.EQ(sqlite.String(projectID.String()))).
			AND(t.Entity.EQ(sqlite.String(string(e)))).
			AND(sqlite.NOT(sqlite.EXISTS(sqlite.SELECT(sqlite.Int(1)).FROM(table).WHERE(id.EQ(t.EntityID).AND(org.EQ(orgArg))))))).
		ExecContext(ctx, tx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *sqliteStore) stateColumns() sqlite.ProjectionList {
	t := s.state
	return sqlite.ProjectionList{t.ProjectID, t.Entity, t.EntityID, t.Version, t.SyncedVersion, t.Deleted, t.Attempts, t.LastError, t.UpdatedAt}
}

// NOTE: SQLite rejects a table-qualified name in RETURNING, so the projections are free-standing columns.
func stateReturning() []sqlite.Projection {
	return []sqlite.Projection{
		sqlite.StringColumn("project_id").AS("opensheet_sync_state.project_id"),
		sqlite.StringColumn("entity").AS("opensheet_sync_state.entity"),
		sqlite.StringColumn("entity_id").AS("opensheet_sync_state.entity_id"),
		sqlite.IntegerColumn("version").AS("opensheet_sync_state.version"),
		sqlite.IntegerColumn("synced_version").AS("opensheet_sync_state.synced_version"),
		sqlite.IntegerColumn("deleted").AS("opensheet_sync_state.deleted"),
		sqlite.IntegerColumn("attempts").AS("opensheet_sync_state.attempts"),
		sqlite.StringColumn("last_error").AS("opensheet_sync_state.last_error"),
		sqlite.StringColumn("updated_at").AS("opensheet_sync_state.updated_at"),
	}
}

// NOTE: one predicate for Claim and ListDirty, so the reconciler never lists a row a job could not claim.
func (s *sqliteStore) claimable(at time.Time) sqlite.BoolExpression {
	t, now := s.state, sqlite.String(sqliteent.SQLiteTime(at))
	return t.SyncedVersion.LT(t.Version).
		AND(t.Attempts.LT(sqlite.Int(MaxRowAttempts))).
		AND(t.LeasedUntil.IS_NULL().OR(t.LeasedUntil.LT(now))).
		AND(t.RetryAfter.IS_NULL().OR(t.RetryAfter.LT(now)))
}

func (s *sqliteStore) Claim(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, token uuid.UUID, at time.Time, ttl time.Duration) (State, bool, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return State{}, false, err
	}
	t := s.state
	var row sqliteStateRow
	err = t.UPDATE().
		SET(t.LeaseToken.SET(sqlite.String(token.String())), t.LeasedUntil.SET(sqlite.String(sqliteent.SQLiteTime(at.Add(ttl))))).
		WHERE(s.stateKey(orgID, projectID, ref).AND(s.claimable(at))).
		RETURNING(stateReturning()...).
		QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return State{}, false, s.endTx(tx, owned, nil)
	}
	if err != nil {
		return State{}, false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.Claim: %w", err))
	}
	st, err := row.toState()
	if err != nil {
		return State{}, false, s.endTx(tx, owned, err)
	}
	return st, true, s.endTx(tx, owned, nil)
}

func (s *sqliteStore) Settle(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, version int64, token uuid.UUID) (settled, dirty bool, err error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return false, false, err
	}
	t := s.state
	var row struct {
		Version int64 `alias:"opensheet_sync_state.version"`
	}
	err = t.UPDATE().
		SET(
			t.SyncedVersion.SET(sqlite.Int(version)),
			t.Attempts.SET(sqlite.Int(0)),
			t.LastError.SET(sqlite.String("")),
			t.LeaseToken.SET(sqliteent.NullText()),
			t.LeasedUntil.SET(sqliteent.NullText()),
			t.RetryAfter.SET(sqliteent.NullText()),
		).
		WHERE(s.stateKey(orgID, projectID, ref).AND(t.LeaseToken.EQ(sqlite.String(token.String())))).
		RETURNING(sqlite.IntegerColumn("version").AS("opensheet_sync_state.version")).
		QueryContext(ctx, tx, &row)
	if errors.Is(err, qrm.ErrNoRows) {
		return false, false, s.endTx(tx, owned, nil)
	}
	if err != nil {
		return false, false, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.Settle: %w", err))
	}
	return true, row.Version > version, s.endTx(tx, owned, nil)
}

func (s *sqliteStore) Release(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, token uuid.UUID, f Failure) error {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return err
	}
	t := s.state
	attempts := sqlite.IntegerExpression(t.Attempts)
	if f.Count {
		attempts = t.Attempts.ADD(sqlite.Int(1))
	}
	retryAfter := sqliteent.NullText()
	if !f.RetryAfter.IsZero() {
		retryAfter = sqlite.String(sqliteent.SQLiteTime(f.RetryAfter))
	}
	// NOTE: a mark since the claim moved the version on; that new edit keeps its fresh attempts and no retry_after.
	if f.Version != 0 {
		same := t.Version.EQ(sqlite.Int(f.Version))
		attempts = sqlite.IntExp(sqlite.CASE().WHEN(same).THEN(attempts).ELSE(t.Attempts))
		retryAfter = sqlite.StringExp(sqlite.CASE().WHEN(same).THEN(retryAfter).ELSE(t.RetryAfter))
	}
	_, err = t.UPDATE().
		SET(
			t.Attempts.SET(attempts),
			t.LastError.SET(sqlite.String(f.Reason)),
			t.LeaseToken.SET(sqliteent.NullText()),
			t.LeasedUntil.SET(sqliteent.NullText()),
			t.RetryAfter.SET(retryAfter),
			t.UpdatedAt.SET(sqlite.String(sqliteent.SQLiteTime(f.At))),
		).
		WHERE(s.stateKey(orgID, projectID, ref).AND(t.LeaseToken.EQ(sqlite.String(token.String())))).
		ExecContext(ctx, tx)
	if err != nil {
		return s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.Release: %w", err))
	}
	return s.endTx(tx, owned, nil)
}

func (s *sqliteStore) ListDirty(ctx context.Context, orgID, projectID uuid.UUID, cutoff, at time.Time, limit int) ([]Ref, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return nil, err
	}
	t := s.state
	var rows []sqliteStateRow
	err = t.SELECT(s.stateColumns()).
		FROM(t).
		WHERE(t.OrgID.EQ(sqlite.String(orgID.String())).
			AND(t.ProjectID.EQ(sqlite.String(projectID.String()))).
			AND(t.UpdatedAt.LT(sqlite.String(sqliteent.SQLiteTime(cutoff)))).
			AND(s.claimable(at))).
		ORDER_BY(t.UpdatedAt.ASC(), t.Entity.ASC(), t.EntityID.ASC()).
		LIMIT(int64(limit)).
		QueryContext(ctx, tx, &rows)
	if err != nil {
		return nil, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.ListDirty: %w", err))
	}
	out := make([]Ref, 0, len(rows))
	for i := range rows {
		st, cErr := rows[i].toState()
		if cErr != nil {
			return nil, s.endTx(tx, owned, cErr)
		}
		out = append(out, st.Ref)
	}
	return out, s.endTx(tx, owned, nil)
}

func (s *sqliteStore) Backlog(ctx context.Context, orgID, projectID uuid.UUID) (Backlog, error) {
	tx, owned, err := s.txAcquire(ctx)
	if err != nil {
		return Backlog{}, err
	}
	t := s.state
	tried, left, spent := t.Attempts.GT(sqlite.Int(0)), t.Attempts.LT(sqlite.Int(MaxRowAttempts)), t.Attempts.GT_EQ(sqlite.Int(MaxRowAttempts))
	count := func(cond sqlite.BoolExpression, alias string) sqlite.Projection {
		return sqlite.COUNT(sqlite.CASE().WHEN(cond).THEN(sqlite.Int(1))).AS(alias)
	}
	var row struct {
		Pending int64 `alias:"backlog.pending"`
		Failing int64 `alias:"backlog.failing"`
		GivenUp int64 `alias:"backlog.given_up"`
	}
	err = sqlite.SELECT(
		count(left, "backlog.pending"),
		count(tried.AND(left), "backlog.failing"),
		count(spent, "backlog.given_up"),
	).FROM(t).
		WHERE(t.OrgID.EQ(sqlite.String(orgID.String())).AND(t.ProjectID.EQ(sqlite.String(projectID.String()))).AND(t.SyncedVersion.LT(t.Version))).
		QueryContext(ctx, tx, &row)
	if err != nil {
		return Backlog{}, s.endTx(tx, owned, fmt.Errorf("opensheetsync.sqlite.Backlog: %w", err))
	}
	return Backlog{Pending: row.Pending, Failing: row.Failing, GivenUp: row.GivenUp}, s.endTx(tx, owned, nil)
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func sqliteTimeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return sqliteent.SQLiteTime(*t)
}

func sqliteTimeExpr(t *time.Time) sqlite.StringExpression {
	if t == nil {
		return sqliteent.NullText()
	}
	return sqlite.String(sqliteent.SQLiteTime(*t))
}

func sqliteUUIDArg(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id.String()
}

func sqliteUUID(s *string) (uuid.UUID, error) {
	if s == nil || *s == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("opensheetsync.sqlite: parse uuid: %w", err)
	}
	return id, nil
}

func sqliteTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("opensheetsync.sqlite: parse time: %w", err)
	}
	return t.UTC(), nil
}

func sqliteTimePtr(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil //nolint:nilnil // a NULL column is no time and no error
	}
	t, err := sqliteTime(*s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
