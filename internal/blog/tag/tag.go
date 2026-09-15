// Package tag is the blog tag subdomain.
package tag

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxNameRunes bounds a tag name.
const MaxNameRunes = 100

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Tag is the aggregate root.
type Tag struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New enforces creation invariants: name trimmed, non-empty, <= MaxNameRunes; slug derived when blank.
func New(orgID, projectID uuid.UUID, name, slug string) (*Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &InvalidNameError{Reason: "empty"}
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return nil, &InvalidNameError{Reason: "over 100 characters"}
	}
	if strings.TrimSpace(slug) == "" {
		slug = name
	}
	slug = slugify(slug)
	if slug == "" {
		return nil, &InvalidNameError{Reason: "slug derives to empty"}
	}
	now := time.Now().UTC()
	return &Tag{
		ID:        uuid.Must(uuid.NewV7()),
		OrgID:     orgID,
		ProjectID: projectID,
		Name:      name,
		Slug:      slug,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Rename replaces the display name, leaving the slug stable so links do not break.
func (t *Tag) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return &InvalidNameError{Reason: "empty"}
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return &InvalidNameError{Reason: "over 100 characters"}
	}
	t.Name = name
	t.UpdatedAt = time.Now().UTC()
	return nil
}

func slugify(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}
