package audit

import (
	"context"
	"database/sql"
	"encoding/json"
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

type postgresStore struct {
	pc    *tenant.PgConn
	table *pgent.AuditEvents
}

func newPostgresStore(pc *tenant.PgConn, schema, prefix string) *postgresStore {
	return &postgresStore{pc: pc, table: pgent.NewAuditEvents(schema, prefix)}
}

func pgUUID(id uuid.UUID) postgres.StringExpression {
	if id == uuid.Nil {
		return pgent.NullUUID()
	}
	return postgres.UUID(id)
}

func (s *postgresStore) insert(ctx context.Context, e Entry) error {
	tx, ok := pdb.CurrentTx(ctx)
	if !ok {
		return &NoUnitOfWorkError{Action: e.Action}
	}
	detail, err := json.Marshal(e.Detail)
	if err != nil {
		return err
	}
	stmt := s.table.INSERT(s.table.AllColumns).VALUES(
		postgres.UUID(e.ID), postgres.UUID(e.OrgID), pgUUID(e.ProjectID), postgres.String(string(e.ActorKind)),
		pgUUID(e.ActorID), postgres.String(e.ActorEmail), postgres.String(e.Action), postgres.String(e.TargetKind),
		postgres.String(e.TargetID), postgres.String(e.Reason), postgres.String(e.RequestID),
		postgres.StringExp(postgres.CAST(postgres.String(string(detail))).AS("jsonb")), postgres.TimestampzT(e.OccurredAt),
	)
	_, err = stmt.ExecContext(ctx, tx)
	return err
}

type pgEntryRow struct {
	ID         uuid.UUID  `alias:"audit_events.id"`
	OrgID      uuid.UUID  `alias:"audit_events.org_id"`
	ProjectID  *uuid.UUID `alias:"audit_events.project_id"`
	ActorKind  string     `alias:"audit_events.actor_kind"`
	ActorID    *uuid.UUID `alias:"audit_events.actor_id"`
	ActorEmail string     `alias:"audit_events.actor_email"`
	Action     string     `alias:"audit_events.action"`
	TargetKind string     `alias:"audit_events.target_kind"`
	TargetID   string     `alias:"audit_events.target_id"`
	Reason     string     `alias:"audit_events.reason"`
	RequestID  string     `alias:"audit_events.request_id"`
	Detail     string     `alias:"audit_events.detail"`
	OccurredAt time.Time  `alias:"audit_events.occurred_at"`
}

func (s *postgresStore) list(ctx context.Context, orgID uuid.UUID, f Filter) ([]Entry, error) {
	tx, ok := pdb.CurrentTx(ctx)
	if !ok {
		tc, err := tenant.From(ctx)
		if err != nil {
			return nil, err
		}
		owned, err := s.pc.BeginTenanted(ctx, tc)
		if err != nil {
			return nil, err
		}
		defer func() { _ = owned.Rollback() }()
		tx = owned
	}
	cond := s.table.OrgID.EQ(postgres.UUID(orgID))
	if !f.Before.IsZero() {
		cond = cond.AND(s.table.OccurredAt.LT(postgres.TimestampzT(f.Before)))
	}
	stmt := postgres.SELECT(s.table.AllColumns).FROM(s.table).WHERE(cond).
		ORDER_BY(s.table.OccurredAt.DESC(), s.table.ID.DESC()).LIMIT(int64(f.Limit))
	var rows []pgEntryRow
	if err := stmt.QueryContext(ctx, tx, &rows); err != nil && !errors.Is(err, qrm.ErrNoRows) && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("audit.postgres: list: %w", err)
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		e := Entry{ID: r.ID, OrgID: r.OrgID, ActorKind: ActorKind(r.ActorKind), ActorEmail: r.ActorEmail, Action: r.Action,
			TargetKind: r.TargetKind, TargetID: r.TargetID, Reason: r.Reason, RequestID: r.RequestID, OccurredAt: r.OccurredAt.UTC()}
		if r.ProjectID != nil {
			e.ProjectID = *r.ProjectID
		}
		if r.ActorID != nil {
			e.ActorID = *r.ActorID
		}
		if err := json.Unmarshal([]byte(r.Detail), &e.Detail); err != nil {
			return nil, fmt.Errorf("audit.postgres: detail: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
}
