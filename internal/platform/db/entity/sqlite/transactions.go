package sqlite

import "github.com/go-jet/jet/v2/sqlite"

// Transactions is the jet binding for the transactions table.
type Transactions struct {
	sqlite.Table

	ID          sqlite.ColumnString
	OrgID       sqlite.ColumnString
	ProjectID   sqlite.ColumnString
	WalletID    sqlite.ColumnString
	ToWalletID  sqlite.ColumnString
	Kind        sqlite.ColumnString
	AmountMinor sqlite.ColumnInteger
	Currency    sqlite.ColumnString
	CategoryID  sqlite.ColumnString
	PeriodID    sqlite.ColumnString
	Note        sqlite.ColumnString
	NoteNorm    sqlite.ColumnString
	OccurredAt  sqlite.ColumnString
	CreatedBy   sqlite.ColumnString
	CreatedAt   sqlite.ColumnString
	UpdatedAt   sqlite.ColumnString

	AllColumns sqlite.ColumnList
}

// NewTransactions builds the transactions binding.
func NewTransactions(tablePrefix string) *Transactions {
	var (
		id          = sqlite.StringColumn("id")
		orgID       = sqlite.StringColumn("org_id")
		projectID   = sqlite.StringColumn("project_id")
		walletID    = sqlite.StringColumn("wallet_id")
		toWalletID  = sqlite.StringColumn("to_wallet_id")
		kind        = sqlite.StringColumn("kind")
		amountMinor = sqlite.IntegerColumn("amount_minor")
		currency    = sqlite.StringColumn("currency")
		categoryID  = sqlite.StringColumn("category_id")
		periodID    = sqlite.StringColumn("period_id")
		note        = sqlite.StringColumn("note")
		noteNorm    = sqlite.StringColumn("note_norm")
		occurredAt  = sqlite.StringColumn("occurred_at")
		createdBy   = sqlite.StringColumn("created_by")
		createdAt   = sqlite.StringColumn("created_at")
		updatedAt   = sqlite.StringColumn("updated_at")
		all         = sqlite.ColumnList{id, orgID, projectID, walletID, toWalletID, kind, amountMinor, currency, categoryID, periodID, note, noteNorm, occurredAt, createdBy, createdAt, updatedAt}
	)
	return &Transactions{
		Table:       sqlite.NewTable("", tablePrefix+"transactions", "transactions", all...),
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
