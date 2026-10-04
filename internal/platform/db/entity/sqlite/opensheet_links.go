package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// OpensheetLinks is the jet binding for the opensheet_links table.
type OpensheetLinks struct {
	sqlite.Table

	ID                sqlite.ColumnString
	OrgID             sqlite.ColumnString
	ProjectID         sqlite.ColumnString
	OSOrg             sqlite.ColumnString
	OSProject         sqlite.ColumnString
	APIKeySealed      sqlite.ColumnBlob
	APIKeyHint        sqlite.ColumnString
	TransactionsSheet sqlite.ColumnString
	WalletsSheet      sqlite.ColumnString
	CategoriesSheet   sqlite.ColumnString
	Enabled           sqlite.ColumnInteger
	VerifiedAt        sqlite.ColumnString
	LastError         sqlite.ColumnString
	LastSyncedAt      sqlite.ColumnString
	FailureStreak     sqlite.ColumnInteger
	AutoDisabledAt    sqlite.ColumnString
	CreatedBy         sqlite.ColumnString
	CreatedByKeyID    sqlite.ColumnString
	CreatedAt         sqlite.ColumnString
	UpdatedAt         sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewOpensheetLinks builds the opensheet_links binding.
func NewOpensheetLinks(tablePrefix string) *OpensheetLinks {
	var (
		id                = sqlite.StringColumn("id")
		orgID             = sqlite.StringColumn("org_id")
		projectID         = sqlite.StringColumn("project_id")
		osOrg             = sqlite.StringColumn("os_org")
		osProject         = sqlite.StringColumn("os_project")
		apiKeySealed      = sqlite.BlobColumn("api_key_sealed")
		apiKeyHint        = sqlite.StringColumn("api_key_hint")
		transactionsSheet = sqlite.StringColumn("transactions_sheet")
		walletsSheet      = sqlite.StringColumn("wallets_sheet")
		categoriesSheet   = sqlite.StringColumn("categories_sheet")
		enabled           = sqlite.IntegerColumn("enabled")
		verifiedAt        = sqlite.StringColumn("verified_at")
		lastError         = sqlite.StringColumn("last_error")
		lastSyncedAt      = sqlite.StringColumn("last_synced_at")
		failureStreak     = sqlite.IntegerColumn("failure_streak")
		autoDisabledAt    = sqlite.StringColumn("auto_disabled_at")
		createdBy         = sqlite.StringColumn("created_by")
		createdByKeyID    = sqlite.StringColumn("created_by_key_id")
		createdAt         = sqlite.StringColumn("created_at")
		updatedAt         = sqlite.StringColumn("updated_at")
		all               = sqlite.ColumnList{
			id, orgID, projectID, osOrg, osProject, apiKeySealed, apiKeyHint,
			transactionsSheet, walletsSheet, categoriesSheet, enabled, verifiedAt, lastError, lastSyncedAt,
			failureStreak, autoDisabledAt, createdBy, createdByKeyID, createdAt, updatedAt,
		}
	)
	return &OpensheetLinks{
		Table: sqlite.NewTable("", tablePrefix+"opensheet_links", "opensheet_links", all...),
		ID:    id, OrgID: orgID, ProjectID: projectID, OSOrg: osOrg, OSProject: osProject,
		APIKeySealed: apiKeySealed, APIKeyHint: apiKeyHint,
		TransactionsSheet: transactionsSheet, WalletsSheet: walletsSheet, CategoriesSheet: categoriesSheet,
		Enabled: enabled, VerifiedAt: verifiedAt, LastError: lastError, LastSyncedAt: lastSyncedAt,
		FailureStreak: failureStreak, AutoDisabledAt: autoDisabledAt,
		CreatedBy: createdBy, CreatedByKeyID: createdByKeyID, CreatedAt: createdAt, UpdatedAt: updatedAt,
		AllColumns: all,
	}
}
