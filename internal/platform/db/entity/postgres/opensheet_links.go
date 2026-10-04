package postgres

import "github.com/go-jet/jet/v2/postgres"

// OpensheetLinks is the jet binding for the opensheet_links table.
type OpensheetLinks struct {
	postgres.Table

	ID                postgres.ColumnString
	OrgID             postgres.ColumnString
	ProjectID         postgres.ColumnString
	OSOrg             postgres.ColumnString
	OSProject         postgres.ColumnString
	APIKeySealed      postgres.ColumnBytea
	APIKeyHint        postgres.ColumnString
	TransactionsSheet postgres.ColumnString
	WalletsSheet      postgres.ColumnString
	CategoriesSheet   postgres.ColumnString
	Enabled           postgres.ColumnBool
	VerifiedAt        postgres.ColumnTimestampz
	LastError         postgres.ColumnString
	LastSyncedAt      postgres.ColumnTimestampz
	FailureStreak     postgres.ColumnInteger
	AutoDisabledAt    postgres.ColumnTimestampz
	CreatedBy         postgres.ColumnString
	CreatedByKeyID    postgres.ColumnString
	CreatedAt         postgres.ColumnTimestampz
	UpdatedAt         postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewOpensheetLinks builds the opensheet_links binding.
func NewOpensheetLinks(schema, tablePrefix string) *OpensheetLinks {
	if schema == "" {
		schema = "public"
	}
	var (
		id                = postgres.StringColumn("id")
		orgID             = postgres.StringColumn("org_id")
		projectID         = postgres.StringColumn("project_id")
		osOrg             = postgres.StringColumn("os_org")
		osProject         = postgres.StringColumn("os_project")
		apiKeySealed      = postgres.ByteaColumn("api_key_sealed")
		apiKeyHint        = postgres.StringColumn("api_key_hint")
		transactionsSheet = postgres.StringColumn("transactions_sheet")
		walletsSheet      = postgres.StringColumn("wallets_sheet")
		categoriesSheet   = postgres.StringColumn("categories_sheet")
		enabled           = postgres.BoolColumn("enabled")
		verifiedAt        = postgres.TimestampzColumn("verified_at")
		lastError         = postgres.StringColumn("last_error")
		lastSyncedAt      = postgres.TimestampzColumn("last_synced_at")
		failureStreak     = postgres.IntegerColumn("failure_streak")
		autoDisabledAt    = postgres.TimestampzColumn("auto_disabled_at")
		createdBy         = postgres.StringColumn("created_by")
		createdByKeyID    = postgres.StringColumn("created_by_key_id")
		createdAt         = postgres.TimestampzColumn("created_at")
		updatedAt         = postgres.TimestampzColumn("updated_at")
		all               = postgres.ColumnList{
			id, orgID, projectID, osOrg, osProject, apiKeySealed, apiKeyHint,
			transactionsSheet, walletsSheet, categoriesSheet, enabled, verifiedAt, lastError, lastSyncedAt,
			failureStreak, autoDisabledAt, createdBy, createdByKeyID, createdAt, updatedAt,
		}
	)
	return &OpensheetLinks{
		Table: postgres.NewTable(schema, tablePrefix+"opensheet_links", "opensheet_links", all...),
		ID:    id, OrgID: orgID, ProjectID: projectID, OSOrg: osOrg, OSProject: osProject,
		APIKeySealed: apiKeySealed, APIKeyHint: apiKeyHint,
		TransactionsSheet: transactionsSheet, WalletsSheet: walletsSheet, CategoriesSheet: categoriesSheet,
		Enabled: enabled, VerifiedAt: verifiedAt, LastError: lastError, LastSyncedAt: lastSyncedAt,
		FailureStreak: failureStreak, AutoDisabledAt: autoDisabledAt,
		CreatedBy: createdBy, CreatedByKeyID: createdByKeyID, CreatedAt: createdAt, UpdatedAt: updatedAt,
		AllColumns: all,
	}
}
