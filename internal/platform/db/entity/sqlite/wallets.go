package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// Wallets is the jet binding for the wallets table.
type Wallets struct {
	sqlite.Table

	ID               sqlite.ColumnString
	OrgID            sqlite.ColumnString
	ProjectID        sqlite.ColumnString
	Name             sqlite.ColumnString
	Kind             sqlite.ColumnString
	Provider         sqlite.ColumnString
	Currency         sqlite.ColumnString
	ExcludeFromTotal sqlite.ColumnInteger
	ArchivedAt       sqlite.ColumnString
	CreatedAt        sqlite.ColumnString
	UpdatedAt        sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewWallets builds the wallets binding.
func NewWallets(tablePrefix string) *Wallets {
	var (
		id               = sqlite.StringColumn("id")
		orgID            = sqlite.StringColumn("org_id")
		projectID        = sqlite.StringColumn("project_id")
		name             = sqlite.StringColumn("name")
		kind             = sqlite.StringColumn("kind")
		provider         = sqlite.StringColumn("provider")
		currency         = sqlite.StringColumn("currency")
		excludeFromTotal = sqlite.IntegerColumn("exclude_from_total")
		archivedAt       = sqlite.StringColumn("archived_at")
		createdAt        = sqlite.StringColumn("created_at")
		updatedAt        = sqlite.StringColumn("updated_at")
		all              = sqlite.ColumnList{id, orgID, projectID, name, kind, provider, currency, excludeFromTotal, archivedAt, createdAt, updatedAt}
	)
	return &Wallets{
		Table:            sqlite.NewTable("", tablePrefix+"wallets", "wallets", all...),
		ID:               id,
		OrgID:            orgID,
		ProjectID:        projectID,
		Name:             name,
		Kind:             kind,
		Provider:         provider,
		Currency:         currency,
		ExcludeFromTotal: excludeFromTotal,
		ArchivedAt:       archivedAt,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
		AllColumns:       all,
	}
}
