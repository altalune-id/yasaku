package postgres

import "github.com/go-jet/jet/v2/postgres"

// Wallets is the jet binding for the wallets table.
type Wallets struct {
	postgres.Table

	ID               postgres.ColumnString
	OrgID            postgres.ColumnString
	ProjectID        postgres.ColumnString
	Name             postgres.ColumnString
	Kind             postgres.ColumnString
	Provider         postgres.ColumnString
	Currency         postgres.ColumnString
	ExcludeFromTotal postgres.ColumnBool
	ArchivedAt       postgres.ColumnTimestampz
	CreatedAt        postgres.ColumnTimestampz
	UpdatedAt        postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewWallets builds the wallets binding.
func NewWallets(schema, tablePrefix string) *Wallets {
	if schema == "" {
		schema = "public"
	}
	var (
		id               = postgres.StringColumn("id")
		orgID            = postgres.StringColumn("org_id")
		projectID        = postgres.StringColumn("project_id")
		name             = postgres.StringColumn("name")
		kind             = postgres.StringColumn("kind")
		provider         = postgres.StringColumn("provider")
		currency         = postgres.StringColumn("currency")
		excludeFromTotal = postgres.BoolColumn("exclude_from_total")
		archivedAt       = postgres.TimestampzColumn("archived_at")
		createdAt        = postgres.TimestampzColumn("created_at")
		updatedAt        = postgres.TimestampzColumn("updated_at")
		all              = postgres.ColumnList{id, orgID, projectID, name, kind, provider, currency, excludeFromTotal, archivedAt, createdAt, updatedAt}
	)
	return &Wallets{
		Table:            postgres.NewTable(schema, tablePrefix+"wallets", "wallets", all...),
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
