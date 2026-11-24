package postgres

import "github.com/go-jet/jet/v2/postgres"

// AuditEvents is the jet binding for the audit_events table.
type AuditEvents struct {
	postgres.Table

	ID         postgres.ColumnString
	OrgID      postgres.ColumnString
	ProjectID  postgres.ColumnString
	ActorKind  postgres.ColumnString
	ActorID    postgres.ColumnString
	ActorEmail postgres.ColumnString
	Action     postgres.ColumnString
	TargetKind postgres.ColumnString
	TargetID   postgres.ColumnString
	Reason     postgres.ColumnString
	RequestID  postgres.ColumnString
	Detail     postgres.ColumnString
	OccurredAt postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewAuditEvents builds the audit_events binding.
func NewAuditEvents(schema, tablePrefix string) *AuditEvents {
	if schema == "" {
		schema = "public"
	}
	var (
		id         = postgres.StringColumn("id")
		orgID      = postgres.StringColumn("org_id")
		projectID  = postgres.StringColumn("project_id")
		actorKind  = postgres.StringColumn("actor_kind")
		actorID    = postgres.StringColumn("actor_id")
		actorEmail = postgres.StringColumn("actor_email")
		action     = postgres.StringColumn("action")
		targetKind = postgres.StringColumn("target_kind")
		targetID   = postgres.StringColumn("target_id")
		reason     = postgres.StringColumn("reason")
		requestID  = postgres.StringColumn("request_id")
		detail     = postgres.StringColumn("detail")
		occurredAt = postgres.TimestampzColumn("occurred_at")
		all        = postgres.ColumnList{id, orgID, projectID, actorKind, actorID, actorEmail, action, targetKind, targetID, reason, requestID, detail, occurredAt}
	)
	return &AuditEvents{
		Table:      postgres.NewTable(schema, tablePrefix+"audit_events", "audit_events", all...),
		ID:         id,
		OrgID:      orgID,
		ProjectID:  projectID,
		ActorKind:  actorKind,
		ActorID:    actorID,
		ActorEmail: actorEmail,
		Action:     action,
		TargetKind: targetKind,
		TargetID:   targetID,
		Reason:     reason,
		RequestID:  requestID,
		Detail:     detail,
		OccurredAt: occurredAt,
		AllColumns: all,
	}
}
