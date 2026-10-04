package opensheetsync

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

//nolint:gochecknoglobals // a compiled pattern, not runtime state.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

const (
	maxAPIKeyLen = 512
	hintLen      = 4
	// DisableAfter is how many link-level sync refusals in a row turn a link off; row refusals never count.
	DisableAfter = 3
)

// SheetSlugs names the opensheet sheet that mirrors each entity.
type SheetSlugs struct {
	Transactions string
	Wallets      string
	Categories   string
}

// For returns the slug mirroring e, or "" for an unknown entity.
func (s SheetSlugs) For(e Entity) string {
	switch e {
	case EntityTransaction:
		return s.Transactions
	case EntityWallet:
		return s.Wallets
	case EntityCategory:
		return s.Categories
	}
	return ""
}

// DefaultSheetSlugs is the slug the tutorial suggests for each tab.
func DefaultSheetSlugs() SheetSlugs {
	var s SheetSlugs
	for _, t := range Contract() {
		switch t.Entity {
		case EntityTransaction:
			s.Transactions = t.DefaultSlug
		case EntityWallet:
			s.Wallets = t.DefaultSlug
		case EntityCategory:
			s.Categories = t.DefaultSlug
		}
	}
	return s
}

// Settings is what a person enters in the form; an empty APIKey keeps the saved key.
type Settings struct {
	OSOrg     string
	OSProject string
	APIKey    string
	Sheets    SheetSlugs
}

const redacted = "[redacted]"

// String renders the settings with the API key redacted, so a %v in a log or an error never leaks it.
func (s Settings) String() string {
	return fmt.Sprintf("{OSOrg:%s OSProject:%s APIKey:%s Sheets:%+v}", s.OSOrg, s.OSProject, s.keyText(), s.Sheets)
}

// GoString renders the settings for %#v with the API key redacted.
func (s Settings) GoString() string { return "opensheetsync.Settings" + s.String() }

// LogValue renders the settings for slog with the API key redacted.
func (s Settings) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("os_org", s.OSOrg), slog.String("os_project", s.OSProject), slog.String("api_key", s.keyText()),
		slog.String("transactions_sheet", s.Sheets.Transactions), slog.String("wallets_sheet", s.Sheets.Wallets),
		slog.String("categories_sheet", s.Sheets.Categories),
	)
}

func (s Settings) keyText() string {
	if s.APIKey == "" {
		return ""
	}
	return redacted
}

// Normalized trims every field.
func (s Settings) Normalized() Settings {
	return Settings{
		OSOrg:     strings.TrimSpace(s.OSOrg),
		OSProject: strings.TrimSpace(s.OSProject),
		APIKey:    strings.TrimSpace(s.APIKey),
		Sheets: SheetSlugs{
			Transactions: strings.TrimSpace(s.Sheets.Transactions),
			Wallets:      strings.TrimSpace(s.Sheets.Wallets),
			Categories:   strings.TrimSpace(s.Sheets.Categories),
		},
	}
}

// Validate checks every slug, that each tab has its own sheet and, when requireKey is set, that a key was entered.
func (s Settings) Validate(requireKey bool) error {
	for _, f := range []struct{ field, value string }{
		{"os_org", s.OSOrg},
		{"os_project", s.OSProject},
		{"transactions_sheet", s.Sheets.Transactions},
		{"wallets_sheet", s.Sheets.Wallets},
		{"categories_sheet", s.Sheets.Categories},
	} {
		if !slugRe.MatchString(f.value) {
			return &InvalidSettingError{Field: f.field, Reason: "use lowercase letters, digits and hyphens, at most 64"}
		}
	}
	sh := s.Sheets
	if sh.Transactions == sh.Wallets || sh.Transactions == sh.Categories || sh.Wallets == sh.Categories {
		return &InvalidSettingError{Field: "sheets", Reason: "each tab needs its own sheet"}
	}
	if s.APIKey == "" {
		if requireKey {
			return &APIKeyRequiredError{}
		}
		return nil
	}
	if utf8.RuneCountInString(s.APIKey) > maxAPIKeyLen || strings.ContainsAny(s.APIKey, " \t\r\n") {
		return &InvalidSettingError{Field: "api_key", Reason: "paste the key exactly as opensheet showed it"}
	}
	return nil
}

// KeyHint is the last four characters of a key, the only part ever shown again; a short key shows nothing.
func KeyHint(key string) string {
	r := []rune(key)
	if len(r) < 3*hintLen {
		return ""
	}
	return string(r[len(r)-hintLen:])
}

// Link is a project's connection to opensheet: where to mirror, the sealed key and whether the mirror runs.
type Link struct {
	ID             uuid.UUID
	OrgID          uuid.UUID
	ProjectID      uuid.UUID
	OSOrg          string
	OSProject      string
	APIKeySealed   []byte
	APIKeyHint     string
	Sheets         SheetSlugs
	Enabled        bool
	VerifiedAt     *time.Time
	LastError      string
	LastSyncedAt   *time.Time
	FailureStreak  int
	AutoDisabledAt *time.Time
	// CreatedBy is the saving user, or uuid.Nil when an API key saved the link.
	CreatedBy uuid.UUID
	// CreatedByKeyID is the saving API key, or uuid.Nil when a user saved the link.
	CreatedByKeyID uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewLink starts a project's link with the default sheet slugs, off and unverified.
func NewLink(orgID, projectID, by, byKey uuid.UUID, at time.Time) *Link {
	at = at.UTC()
	return &Link{
		ID: uuid.New(), OrgID: orgID, ProjectID: projectID, Sheets: DefaultSheetSlugs(),
		CreatedBy: by, CreatedByKeyID: byKey, CreatedAt: at, UpdatedAt: at,
	}
}

// Configure applies settings that just passed the Test, starts a fresh failure streak and clears AutoDisabledAt, keeping LastError; a nil sealed keeps the saved key.
func (l *Link) Configure(s Settings, sealed []byte, hint string, at time.Time) {
	at = at.UTC()
	l.OSOrg, l.OSProject, l.Sheets = s.OSOrg, s.OSProject, s.Sheets
	if sealed != nil {
		l.APIKeySealed, l.APIKeyHint = sealed, hint
	}
	l.VerifiedAt = &at
	l.FailureStreak = 0
	l.AutoDisabledAt = nil
	l.UpdatedAt = at
}

// Retargets reports whether s points the mirror at other sheets, which then need a full backfill.
func (l *Link) Retargets(s Settings) bool {
	return l.OSOrg != s.OSOrg || l.OSProject != s.OSProject || l.Sheets != s.Sheets
}

// Settings returns the saved settings without the key.
func (l *Link) Settings() Settings {
	return Settings{OSOrg: l.OSOrg, OSProject: l.OSProject, Sheets: l.Sheets}
}

// Enable turns the mirror on and clears the failure streak; a link not verified since its last auto-disable is refused.
func (l *Link) Enable(at time.Time) error {
	if l.VerifiedAt == nil {
		return &NotVerifiedError{}
	}
	l.Enabled = true
	l.FailureStreak = 0
	l.AutoDisabledAt = nil
	l.LastError = ""
	l.UpdatedAt = at.UTC()
	return nil
}

// Disable turns the mirror off; the link and its sync state stay.
func (l *Link) Disable(at time.Time) {
	l.Enabled = false
	l.UpdatedAt = at.UTC()
}

// AutoDisabled reports whether repeated failures, not a person, turned the mirror off.
func (l *Link) AutoDisabled() bool { return l.AutoDisabledAt != nil }

// AwaitingFirstSync reports a saved link no sync has landed on since its last Save, so columns the Test could not see are still unconfirmed.
func (l *Link) AwaitingFirstSync() bool {
	return l.VerifiedAt != nil && (l.LastSyncedAt == nil || l.LastSyncedAt.Before(*l.VerifiedAt))
}
