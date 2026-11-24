package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-jet/jet/v2/qrm"
	"github.com/go-jet/jet/v2/sqlite"
	"github.com/google/uuid"

	pdb "altalune.id/yasaku/internal/platform/db"
	sqliteent "altalune.id/yasaku/internal/platform/db/entity/sqlite"
)

type sqliteStore struct {
	db    *sql.DB
	table *sqliteent.AuditEvents
}

func newSQLiteStore(db *sql.DB, prefix string) *sqliteStore {
	return &sqliteStore{db: db, table: sqliteent.NewAuditEvents(prefix)}
}

func sqliteUUID(id uuid.UUID) sqlite.StringExpression {
	if id == uuid.Nil {
		return sqliteent.NullText()
	}
	return sqlite.String(id.String())
}

func (s *sqliteStore) insert(ctx context.Context, e Entry) error {
	tx, ok := pdb.CurrentTx(ctx)
	if !ok {
		return &NoUnitOfWorkError{Action: e.Action}
	}
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return err
	}
	stmt := s.table.INSERT(s.table.AllColumns).VALUES(
		sqlite.String(e.ID.String()), sqlite.String(e.OrgID.String()), sqliteUUID(e.ProjectID), sqlite.String(string(e.ActorKind)),
		sqliteUUID(e.ActorID), sqlite.String(e.ActorEmail), sqlite.String(e.Action), sqlite.String(e.TargetKind),
		sqlite.String(e.TargetID), sqlite.String(e.Reason), sqlite.String(e.RequestID), sqlite.String(string(detail)),
		sqlite.String(sqliteent.SQLiteTime(e.OccurredAt)),
	)
	_, err = stmt.ExecContext(ctx, tx)
	return err
}

type sqliteEntryRow struct {
	ID         string  `alias:"audit_events.id"`
	OrgID      string  `alias:"audit_events.org_id"`
	ProjectID  *string `alias:"audit_events.project_id"`
	ActorKind  string  `alias:"audit_events.actor_kind"`
	ActorID    *string `alias:"audit_events.actor_id"`
	ActorEmail string  `alias:"audit_events.actor_email"`
	Action     string  `alias:"audit_events.action"`
	TargetKind string  `alias:"audit_events.target_kind"`
	TargetID   string  `alias:"audit_events.target_id"`
	Reason     string  `alias:"audit_events.reason"`
	RequestID  string  `alias:"audit_events.request_id"`
	Detail     string  `alias:"audit_events.detail"`
	OccurredAt string  `alias:"audit_events.occurred_at"`
}

func (s *sqliteStore) conn(ctx context.Context) qrm.DB {
	if tx, ok := pdb.CurrentTx(ctx); ok {
		return tx
	}
	return s.db
}

func (s *sqliteStore) list(ctx context.Context, orgID uuid.UUID, f Filter) ([]Entry, error) {
	cond := s.table.OrgID.EQ(sqlite.String(orgID.String()))
	if !f.Before.IsZero() {
		cond = cond.AND(s.table.OccurredAt.LT(sqlite.String(sqliteent.SQLiteTime(f.Before))))
	}
	stmt := sqlite.SELECT(s.table.AllColumns).FROM(s.table).WHERE(cond).
		ORDER_BY(s.table.OccurredAt.DESC(), s.table.ID.DESC()).LIMIT(int64(f.Limit))
	var rows []sqliteEntryRow
	if err := stmt.QueryContext(ctx, s.conn(ctx), &rows); err != nil && !errors.Is(err, qrm.ErrNoRows) && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("audit.sqlite: list: %w", err)
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		e, err := r.entry()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (r sqliteEntryRow) entry() (Entry, error) {
	e := Entry{ActorKind: ActorKind(r.ActorKind), ActorEmail: r.ActorEmail, Action: r.Action, TargetKind: r.TargetKind,
		TargetID: r.TargetID, Reason: r.Reason, RequestID: r.RequestID}
	var err error
	if e.ID, err = uuid.Parse(r.ID); err != nil {
		return Entry{}, fmt.Errorf("audit.sqlite: id: %w", err)
	}
	if e.OrgID, err = uuid.Parse(r.OrgID); err != nil {
		return Entry{}, fmt.Errorf("audit.sqlite: org id: %w", err)
	}
	if r.ProjectID != nil {
		if e.ProjectID, err = uuid.Parse(*r.ProjectID); err != nil {
			return Entry{}, fmt.Errorf("audit.sqlite: project id: %w", err)
		}
	}
	if r.ActorID != nil {
		if e.ActorID, err = uuid.Parse(*r.ActorID); err != nil {
			return Entry{}, fmt.Errorf("audit.sqlite: actor id: %w", err)
		}
	}
	if e.OccurredAt, err = time.Parse(time.RFC3339Nano, r.OccurredAt); err != nil {
		return Entry{}, fmt.Errorf("audit.sqlite: occurred at: %w", err)
	}
	if err := json.Unmarshal([]byte(r.Detail), &e.Detail); err != nil {
		return Entry{}, fmt.Errorf("audit.sqlite: detail: %w", err)
	}
	return e, nil
}
