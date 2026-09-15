// Package category is the transaction categories bounded context.
package category

import (
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxNameRunes bounds a category name.
const MaxNameRunes = 100

// MaxSortOrder bounds SortOrder to what the sort_order INTEGER column holds.
const MaxSortOrder = math.MaxInt32

// Kind splits categories into the two transaction directions they may classify.
type Kind string

// The two category kinds. Transfers and balance adjustments carry no category.
const (
	KindExpense Kind = "expense"
	KindIncome  Kind = "income"
)

// AllowedIcons is the fixed allow-list of icon names a category may carry.
//
//nolint:gochecknoglobals // a fixed allow-list, not runtime state.
var AllowedIcons = []string{
	"utensils", "bus", "shopping-bag", "receipt", "smartphone", "heart-pulse", "house",
	"clapperboard", "graduation-cap", "users", "credit-card", "hand-heart", "circle-ellipsis",
	"banknote", "gift", "briefcase", "trending-up", "laptop", "wallet",
}

//nolint:gochecknoglobals // compiled regexps are package-level fixtures, not runtime state.
var (
	hexColor   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	chartColor = regexp.MustCompile(`^chart-[1-5]$`)
)

// ParseKind resolves a wire or user-supplied string to a Kind.
func ParseKind(s string) (Kind, error) {
	k := Kind(strings.ToLower(strings.TrimSpace(s)))
	switch k {
	case KindExpense, KindIncome:
		return k, nil
	default:
		return "", &InvalidKindError{Value: s}
	}
}

// Category is the aggregate root. Invariants live here.
type Category struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	ProjectID  uuid.UUID
	Name       string
	Kind       Kind
	Icon       string
	Color      string
	SortOrder  int
	ArchivedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// New enforces creation invariants: name trimmed and non-empty, known kind, allow-listed icon, accepted colour.
func New(orgID, projectID uuid.UUID, name string, kind Kind, icon, color string, sortOrder int) (*Category, error) {
	name, err := normalizeName(name)
	if err != nil {
		return nil, err
	}
	if kind != KindExpense && kind != KindIncome {
		return nil, &InvalidKindError{Value: string(kind)}
	}
	icon, err = normalizeIcon(icon)
	if err != nil {
		return nil, err
	}
	color, err = normalizeColor(color)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Category{
		ID:        uuid.Must(uuid.NewV7()),
		OrgID:     orgID,
		ProjectID: projectID,
		Name:      name,
		Kind:      kind,
		Icon:      icon,
		Color:     color,
		SortOrder: clampSortOrder(sortOrder),
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Rename replaces the display name. The kind is immutable: it decides which transactions may use the category.
func (c *Category) Rename(name string) error {
	name, err := normalizeName(name)
	if err != nil {
		return err
	}
	c.Name = name
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// Update replaces icon and colour together, leaving both untouched when either is invalid.
func (c *Category) Update(icon, color string) error {
	icon, err := normalizeIcon(icon)
	if err != nil {
		return err
	}
	color, err = normalizeColor(color)
	if err != nil {
		return err
	}
	c.Icon = icon
	c.Color = color
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// Archive hides the category from pickers without deleting its history. Idempotent.
func (c *Category) Archive() {
	if c.ArchivedAt != nil {
		return
	}
	now := time.Now().UTC()
	c.ArchivedAt = &now
	c.UpdatedAt = now
}

// Unarchive returns the category to the pickers. Idempotent.
func (c *Category) Unarchive() {
	if c.ArchivedAt == nil {
		return
	}
	c.ArchivedAt = nil
	c.UpdatedAt = time.Now().UTC()
}

// IsArchived reports whether the category is hidden from pickers.
func (c *Category) IsArchived() bool { return c.ArchivedAt != nil }

// ListOpts filters Store.List. Zero value returns every active category in scope.
type ListOpts struct {
	Kind            Kind
	IncludeArchived bool
}

func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &InvalidNameError{Reason: "empty"}
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return "", &InvalidNameError{Reason: "over 100 characters"}
	}
	return name, nil
}

func normalizeIcon(icon string) (string, error) {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return "", nil
	}
	if !slices.Contains(AllowedIcons, icon) {
		return "", &InvalidIconError{Icon: icon}
	}
	return icon, nil
}

func normalizeColor(color string) (string, error) {
	color = strings.TrimSpace(color)
	switch {
	case color == "":
		return "", nil
	case chartColor.MatchString(color):
		return color, nil
	case hexColor.MatchString(color):
		return strings.ToLower(color), nil
	default:
		return "", &InvalidColorError{Color: color}
	}
}

func clampSortOrder(v int) int {
	return min(max(v, 0), MaxSortOrder)
}

// SortOrder32 narrows a category's sort order to the width of the sort_order column.
func SortOrder32(v int) int32 {
	return int32(clampSortOrder(v)) //nolint:gosec // clampSortOrder bounds v to [0, math.MaxInt32].
}

// FoldName is the case-insensitive form the partial unique index compares on.
func FoldName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func titleCase(key string) string {
	words := strings.FieldsFunc(key, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, w := range words {
		runes := []rune(w)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
