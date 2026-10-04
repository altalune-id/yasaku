package opensheetsync

import "strings"

const deletedAtColumn = "deleted_at"

// Tab is one sheet yasaku mirrors into: the entity, the slug the tutorial suggests and the header row it needs.
type Tab struct {
	Entity      Entity
	DefaultSlug string
	Columns     []string
}

// Contract is the sheet contract, in tutorial order. NOTE: changing it breaks every linked sheet; TestContract_IsPinned guards it.
func Contract() []Tab {
	return []Tab{
		{Entity: EntityTransaction, DefaultSlug: "yasaku-transactions", Columns: []string{
			"id", "date", "occurred_at", "kind", "amount", "currency", "wallet", "wallet_id", "to_wallet", "to_wallet_id",
			"category", "category_id", "note", "period", "recurring_id", "updated_at", deletedAtColumn,
		}},
		{Entity: EntityWallet, DefaultSlug: "yasaku-wallets", Columns: []string{
			"id", "name", "kind", "provider", "currency", "balance", "exclude_from_total", "archived", "updated_at", deletedAtColumn,
		}},
		{Entity: EntityCategory, DefaultSlug: "yasaku-categories", Columns: []string{
			"id", "name", "kind", "icon", "color", "archived", "updated_at", deletedAtColumn,
		}},
	}
}

// TabFor returns the contract tab mirroring e.
func TabFor(e Entity) (Tab, bool) {
	for _, t := range Contract() {
		if t.Entity == e {
			return t, true
		}
	}
	return Tab{}, false
}

// HeaderTSV is the header row tab-separated, so one paste fills one row of cells.
func (t Tab) HeaderTSV() string { return strings.Join(t.Columns, "\t") }

// Written is every column yasaku writes; opensheet alone writes deleted_at.
func (t Tab) Written() []string {
	out := make([]string, 0, len(t.Columns))
	for _, c := range t.Columns {
		if c != deletedAtColumn {
			out = append(out, c)
		}
	}
	return out
}
