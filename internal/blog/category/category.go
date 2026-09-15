// Package category is the blog category subdomain.
package category

import (
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxNameRunes bounds a category name.
const MaxNameRunes = 100

//nolint:gochecknoglobals // compiled regexp is a package-level fixture, not runtime state.
var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Category is the aggregate root.
type Category struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New enforces creation invariants: name trimmed, non-empty, <= MaxNameRunes; slug derived when blank.
func New(orgID, projectID uuid.UUID, name, slug string) (*Category, error) {
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
	return &Category{
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
func (c *Category) Rename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return &InvalidNameError{Reason: "empty"}
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return &InvalidNameError{Reason: "over 100 characters"}
	}
	c.Name = name
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func slugify(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}

var _ = http.StatusOK
