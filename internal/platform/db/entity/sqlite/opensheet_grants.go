package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// OpensheetGrants is the jet binding for the opensheet_grants table.
type OpensheetGrants struct {
	sqlite.Table

	OrgID           sqlite.ColumnString
	ProjectID       sqlite.ColumnString
	ConnectedBy     sqlite.ColumnString
	RefreshSealed   sqlite.ColumnBlob
	AccessSealed    sqlite.ColumnBlob
	AccessExpiresAt sqlite.ColumnString
	Scopes          sqlite.ColumnString
	Status          sqlite.ColumnString
	CreatedAt       sqlite.ColumnString
	UpdatedAt       sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewOpensheetGrants builds the opensheet_grants binding.
func NewOpensheetGrants(tablePrefix string) *OpensheetGrants {
	var (
		orgID           = sqlite.StringColumn("org_id")
		projectID       = sqlite.StringColumn("project_id")
		connectedBy     = sqlite.StringColumn("connected_by")
		refreshSealed   = sqlite.BlobColumn("refresh_sealed")
		accessSealed    = sqlite.BlobColumn("access_sealed")
		accessExpiresAt = sqlite.StringColumn("access_expires_at")
		scopes          = sqlite.StringColumn("scopes")
		status          = sqlite.StringColumn("status")
		createdAt       = sqlite.StringColumn("created_at")
		updatedAt       = sqlite.StringColumn("updated_at")
		all             = sqlite.ColumnList{
			orgID, projectID, connectedBy, refreshSealed, accessSealed, accessExpiresAt, scopes, status, createdAt, updatedAt,
		}
	)
	return &OpensheetGrants{
		Table: sqlite.NewTable("", tablePrefix+"opensheet_grants", "opensheet_grants", all...),
		OrgID: orgID, ProjectID: projectID, ConnectedBy: connectedBy, RefreshSealed: refreshSealed,
		AccessSealed: accessSealed, AccessExpiresAt: accessExpiresAt, Scopes: scopes, Status: status,
		CreatedAt: createdAt, UpdatedAt: updatedAt, AllColumns: all,
	}
}
