package opensheetsync

import (
	"strconv"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
)

// TransactionRow maps a transaction onto the transactions tab: date is the ledger day in the project zone, occurred_at carries that zone's offset, updated_at is UTC, money is in major units.
func TransactionRow(f TransactionFacts) map[string]string {
	loc := f.Location
	if loc == nil {
		loc = time.UTC
	}
	return map[string]string{
		"id":           f.ID.String(),
		"date":         civil.DateOf(f.OccurredAt, loc).String(),
		"occurred_at":  f.OccurredAt.In(loc).Format(time.RFC3339),
		"kind":         f.Kind,
		"amount":       f.Amount.Major(),
		"currency":     string(f.Amount.Currency),
		"wallet":       f.WalletName,
		"wallet_id":    f.WalletID.String(),
		"to_wallet":    f.ToWalletName,
		"to_wallet_id": idText(f.ToWalletID),
		"category":     f.CategoryName,
		"category_id":  idText(f.CategoryID),
		"note":         f.Note,
		"period":       f.PeriodName,
		"recurring_id": idText(f.RecurringID),
		"updated_at":   instantText(f.UpdatedAt),
	}
}

// WalletRow maps a wallet onto the wallets tab.
func WalletRow(f WalletFacts) map[string]string {
	return map[string]string{
		"id":                 f.ID.String(),
		"name":               f.Name,
		"kind":               f.Kind,
		"provider":           f.Provider,
		"currency":           string(f.Balance.Currency),
		"balance":            f.Balance.Major(),
		"exclude_from_total": strconv.FormatBool(f.ExcludeFromTotal),
		"archived":           strconv.FormatBool(f.Archived),
		"updated_at":         instantText(f.UpdatedAt),
	}
}

// CategoryRow maps a category onto the categories tab.
func CategoryRow(f CategoryFacts) map[string]string {
	return map[string]string{
		"id":         f.ID.String(),
		"name":       f.Name,
		"kind":       f.Kind,
		"icon":       f.Icon,
		"color":      f.Color,
		"archived":   strconv.FormatBool(f.Archived),
		"updated_at": instantText(f.UpdatedAt),
	}
}

func idText(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func instantText(t time.Time) string { return t.UTC().Format(time.RFC3339) }
