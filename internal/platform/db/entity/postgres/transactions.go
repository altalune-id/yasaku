package postgres

import "github.com/go-jet/jet/v2/postgres"

// Transactions is the jet binding for the transactions table.
type Transactions struct {
	postgres.Table

	ID          postgres.ColumnString
	OrgID       postgres.ColumnString
	ProjectID   postgres.ColumnString
	WalletID    postgres.ColumnString
	ToWalletID  postgres.ColumnString
	Kind        postgres.ColumnString
	AmountMinor postgres.ColumnInteger
	Currency    postgres.ColumnString
	CategoryID  postgres.ColumnString
	PeriodID    postgres.ColumnString
	Note        postgres.ColumnString
	NoteNorm    postgres.ColumnString
	OccurredAt  postgres.ColumnTimestampz
	CreatedBy   postgres.ColumnString
	CreatedAt   postgres.ColumnTimestampz
	UpdatedAt   postgres.ColumnTimestampz

	AllColumns postgres.ColumnList
}

// NewTransactions builds the transactions binding.
func NewTransactions(schema, tablePrefix string) *Transactions {
	if schema == "" {
		schema = "public"
	}
	var (
		id          = postgres.StringColumn("id")
		orgID       = postgres.StringColumn("org_id")
		projectID   = postgres.StringColumn("project_id")
		walletID    = postgres.StringColumn("wallet_id")
		toWalletID  = postgres.StringColumn("to_wallet_id")
		kind        = postgres.StringColumn("kind")
		amountMinor = postgres.IntegerColumn("amount_minor")
		currency    = postgres.StringColumn("currency")
		categoryID  = postgres.StringColumn("category_id")
		periodID    = postgres.StringColumn("period_id")
		note        = postgres.StringColumn("note")
		noteNorm    = postgres.StringColumn("note_norm")
		occurredAt  = postgres.TimestampzColumn("occurred_at")
		createdBy   = postgres.StringColumn("created_by")
		createdAt   = postgres.TimestampzColumn("created_at")
		updatedAt   = postgres.TimestampzColumn("updated_at")
		all         = postgres.ColumnList{id, orgID, projectID, walletID, toWalletID, kind, amountMinor, currency, categoryID, periodID, note, noteNorm, occurredAt, createdBy, createdAt, updatedAt}
	)
	return &Transactions{
		Table:       postgres.NewTable(schema, tablePrefix+"transactions", "transactions", all...),
		ID:          id,
		OrgID:       orgID,
		ProjectID:   projectID,
		WalletID:    walletID,
		ToWalletID:  toWalletID,
		Kind:        kind,
		AmountMinor: amountMinor,
		Currency:    currency,
		CategoryID:  categoryID,
		PeriodID:    periodID,
		Note:        note,
		NoteNorm:    noteNorm,
		OccurredAt:  occurredAt,
		CreatedBy:   createdBy,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		AllColumns:  all,
	}
}
