package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// OpensheetSyncState is the jet binding for the opensheet_sync_state table.
type OpensheetSyncState struct {
	sqlite.Table

	OrgID         sqlite.ColumnString
	ProjectID     sqlite.ColumnString
	Entity        sqlite.ColumnString
	EntityID      sqlite.ColumnString
	Version       sqlite.ColumnInteger
	SyncedVersion sqlite.ColumnInteger
	Deleted       sqlite.ColumnInteger
	Attempts      sqlite.ColumnInteger
	LastError     sqlite.ColumnString
	LeaseToken    sqlite.ColumnString
	LeasedUntil   sqlite.ColumnString
	RetryAfter    sqlite.ColumnString
	UpdatedAt     sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewOpensheetSyncState builds the opensheet_sync_state binding.
func NewOpensheetSyncState(tablePrefix string) *OpensheetSyncState {
	var (
		orgID         = sqlite.StringColumn("org_id")
		projectID     = sqlite.StringColumn("project_id")
		entity        = sqlite.StringColumn("entity")
		entityID      = sqlite.StringColumn("entity_id")
		version       = sqlite.IntegerColumn("version")
		syncedVersion = sqlite.IntegerColumn("synced_version")
		deleted       = sqlite.IntegerColumn("deleted")
		attempts      = sqlite.IntegerColumn("attempts")
		lastError     = sqlite.StringColumn("last_error")
		leaseToken    = sqlite.StringColumn("lease_token")
		leasedUntil   = sqlite.StringColumn("leased_until")
		retryAfter    = sqlite.StringColumn("retry_after")
		updatedAt     = sqlite.StringColumn("updated_at")
		all           = sqlite.ColumnList{orgID, projectID, entity, entityID, version, syncedVersion, deleted, attempts, lastError, leaseToken, leasedUntil, retryAfter, updatedAt}
	)
	return &OpensheetSyncState{
		Table: sqlite.NewTable("", tablePrefix+"opensheet_sync_state", "opensheet_sync_state", all...),
		OrgID: orgID, ProjectID: projectID, Entity: entity, EntityID: entityID,
		Version: version, SyncedVersion: syncedVersion, Deleted: deleted, Attempts: attempts,
		LastError: lastError, LeaseToken: leaseToken, LeasedUntil: leasedUntil, RetryAfter: retryAfter, UpdatedAt: updatedAt,
		AllColumns: all,
	}
}
