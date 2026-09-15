package category

// DefaultKeyPrefix is the i18n message-id prefix every default category name resolves under.
const DefaultKeyPrefix = "category.default."

// DefaultNameKey returns the i18n message id carrying the localized name for a default's key.
func DefaultNameKey(key string) string { return DefaultKeyPrefix + key }

// Default describes one seeded category. Key is the i18n suffix, e.g. "food".
type Default struct {
	Key   string
	Kind  Kind
	Icon  string
	Color string
}

// Defaults is the seeded category set in display order: 13 expense kinds, then 7 income kinds.
//
//nolint:gochecknoglobals // a fixed table, not runtime state.
var Defaults = []Default{
	{Key: "food", Kind: KindExpense, Icon: "utensils", Color: "chart-1"},
	{Key: "transport", Kind: KindExpense, Icon: "bus", Color: "chart-2"},
	{Key: "shopping", Kind: KindExpense, Icon: "shopping-bag", Color: "chart-3"},
	{Key: "bills", Kind: KindExpense, Icon: "receipt", Color: "chart-4"},
	{Key: "phone_internet", Kind: KindExpense, Icon: "smartphone", Color: "chart-5"},
	{Key: "health", Kind: KindExpense, Icon: "heart-pulse", Color: "chart-1"},
	{Key: "home", Kind: KindExpense, Icon: "house", Color: "chart-2"},
	{Key: "entertainment", Kind: KindExpense, Icon: "clapperboard", Color: "chart-3"},
	{Key: "education", Kind: KindExpense, Icon: "graduation-cap", Color: "chart-4"},
	{Key: "family", Kind: KindExpense, Icon: "users", Color: "chart-5"},
	{Key: "installments", Kind: KindExpense, Icon: "credit-card", Color: "chart-1"},
	{Key: "charity", Kind: KindExpense, Icon: "hand-heart", Color: "chart-2"},
	{Key: "other_expense", Kind: KindExpense, Icon: "circle-ellipsis", Color: "chart-3"},
	{Key: "salary", Kind: KindIncome, Icon: "banknote", Color: "chart-4"},
	{Key: "bonus", Kind: KindIncome, Icon: "wallet", Color: "chart-5"},
	{Key: "freelance", Kind: KindIncome, Icon: "laptop", Color: "chart-1"},
	{Key: "business", Kind: KindIncome, Icon: "briefcase", Color: "chart-2"},
	{Key: "investment", Kind: KindIncome, Icon: "trending-up", Color: "chart-3"},
	{Key: "gift", Kind: KindIncome, Icon: "gift", Color: "chart-4"},
	{Key: "other_income", Kind: KindIncome, Icon: "circle-ellipsis", Color: "chart-5"},
}
