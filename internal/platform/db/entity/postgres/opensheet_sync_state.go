package postgres

import "github.com/go-jet/jet/v2/postgres"

// OpensheetSyncState is the jet binding for the opensheet_sync_state table.
type OpensheetSyncState struct {
	postgres.Table

	OrgID         postgres.ColumnString
	ProjectID     postgres.ColumnString
	Entity        postgres.ColumnString
	EntityID      postgres.ColumnString
	Version       postgres.ColumnInteger
	SyncedVersion postgres.ColumnInteger
	Deleted       postgres.ColumnBool
	Attempts      postgres.ColumnInteger
	LastError     postgres.ColumnString
	LeaseToken    postgres.ColumnString
	LeasedUntil   postgres.ColumnTimestampz
	RetryAfter    postgres.ColumnTimestampz
	UpdatedAt     postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewOpensheetSyncState builds the opensheet_sync_state binding.
func NewOpensheetSyncState(schema, tablePrefix string) *OpensheetSyncState {
	if schema == "" {
		schema = "public"
	}
	var (
		orgID         = postgres.StringColumn("org_id")
		projectID     = postgres.StringColumn("project_id")
		entity        = postgres.StringColumn("entity")
		entityID      = postgres.StringColumn("entity_id")
		version       = postgres.IntegerColumn("version")
		syncedVersion = postgres.IntegerColumn("synced_version")
		deleted       = postgres.BoolColumn("deleted")
		attempts      = postgres.IntegerColumn("attempts")
		lastError     = postgres.StringColumn("last_error")
		leaseToken    = postgres.StringColumn("lease_token")
		leasedUntil   = postgres.TimestampzColumn("leased_until")
		retryAfter    = postgres.TimestampzColumn("retry_after")
		updatedAt     = postgres.TimestampzColumn("updated_at")
		all           = postgres.ColumnList{orgID, projectID, entity, entityID, version, syncedVersion, deleted, attempts, lastError, leaseToken, leasedUntil, retryAfter, updatedAt}
	)
	return &OpensheetSyncState{
		Table: postgres.NewTable(schema, tablePrefix+"opensheet_sync_state", "opensheet_sync_state", all...),
		OrgID: orgID, ProjectID: projectID, Entity: entity, EntityID: entityID,
		Version: version, SyncedVersion: syncedVersion, Deleted: deleted, Attempts: attempts,
		LastError: lastError, LeaseToken: leaseToken, LeasedUntil: leasedUntil, RetryAfter: retryAfter, UpdatedAt: updatedAt,
		AllColumns: all,
	}
}
