package postgres

import "github.com/go-jet/jet/v2/postgres"

// OpensheetGrants is the jet binding for the opensheet_grants table.
type OpensheetGrants struct {
	postgres.Table

	OrgID           postgres.ColumnString
	ProjectID       postgres.ColumnString
	ConnectedBy     postgres.ColumnString
	RefreshSealed   postgres.ColumnBytea
	AccessSealed    postgres.ColumnBytea
	AccessExpiresAt postgres.ColumnTimestampz
	Scopes          postgres.ColumnString
	Status          postgres.ColumnString
	CreatedAt       postgres.ColumnTimestampz
	UpdatedAt       postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewOpensheetGrants builds the opensheet_grants binding.
func NewOpensheetGrants(schema, tablePrefix string) *OpensheetGrants {
	if schema == "" {
		schema = "public"
	}
	var (
		orgID           = postgres.StringColumn("org_id")
		projectID       = postgres.StringColumn("project_id")
		connectedBy     = postgres.StringColumn("connected_by")
		refreshSealed   = postgres.ByteaColumn("refresh_sealed")
		accessSealed    = postgres.ByteaColumn("access_sealed")
		accessExpiresAt = postgres.TimestampzColumn("access_expires_at")
		scopes          = postgres.StringColumn("scopes")
		status          = postgres.StringColumn("status")
		createdAt       = postgres.TimestampzColumn("created_at")
		updatedAt       = postgres.TimestampzColumn("updated_at")
		all             = postgres.ColumnList{
			orgID, projectID, connectedBy, refreshSealed, accessSealed, accessExpiresAt, scopes, status, createdAt, updatedAt,
		}
	)
	return &OpensheetGrants{
		Table: postgres.NewTable(schema, tablePrefix+"opensheet_grants", "opensheet_grants", all...),
		OrgID: orgID, ProjectID: projectID, ConnectedBy: connectedBy, RefreshSealed: refreshSealed,
		AccessSealed: accessSealed, AccessExpiresAt: accessExpiresAt, Scopes: scopes, Status: status,
		CreatedAt: createdAt, UpdatedAt: updatedAt, AllColumns: all,
	}
}
