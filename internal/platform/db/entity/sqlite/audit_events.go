package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// AuditEvents is the jet binding for the audit_events table.
type AuditEvents struct {
	sqlite.Table

	ID         sqlite.ColumnString
	OrgID      sqlite.ColumnString
	ProjectID  sqlite.ColumnString
	ActorKind  sqlite.ColumnString
	ActorID    sqlite.ColumnString
	ActorEmail sqlite.ColumnString
	Action     sqlite.ColumnString
	TargetKind sqlite.ColumnString
	TargetID   sqlite.ColumnString
	Reason     sqlite.ColumnString
	RequestID  sqlite.ColumnString
	Detail     sqlite.ColumnString
	OccurredAt sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewAuditEvents builds the audit_events binding.
func NewAuditEvents(tablePrefix string) *AuditEvents {
	var (
		id         = sqlite.StringColumn("id")
		orgID      = sqlite.StringColumn("org_id")
		projectID  = sqlite.StringColumn("project_id")
		actorKind  = sqlite.StringColumn("actor_kind")
		actorID    = sqlite.StringColumn("actor_id")
		actorEmail = sqlite.StringColumn("actor_email")
		action     = sqlite.StringColumn("action")
		targetKind = sqlite.StringColumn("target_kind")
		targetID   = sqlite.StringColumn("target_id")
		reason     = sqlite.StringColumn("reason")
		requestID  = sqlite.StringColumn("request_id")
		detail     = sqlite.StringColumn("detail")
		occurredAt = sqlite.StringColumn("occurred_at")
		all        = sqlite.ColumnList{id, orgID, projectID, actorKind, actorID, actorEmail, action, targetKind, targetID, reason, requestID, detail, occurredAt}
	)
	return &AuditEvents{
		Table:      sqlite.NewTable("", tablePrefix+"audit_events", "audit_events", all...),
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
