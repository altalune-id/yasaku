// Package blog is the blog posts bounded context.
package blog

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Status is the publication state of a post.
type Status string

// StatusDraft and StatusPublished are the only publication states.
const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
)

// MaxTitleRunes bounds a post title.
const MaxTitleRunes = 200

// MaxSlugRunes bounds a post slug.
const MaxSlugRunes = 200

// MaxBodyBytes bounds a post body.
const MaxBodyBytes = 64 << 10

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Post is the aggregate root. Invariants live here.
type Post struct {
	ID               uuid.UUID
	OrgID            uuid.UUID
	ProjectID        uuid.UUID
	CategoryID       uuid.UUID
	TagIDs           []uuid.UUID
	Title            string
	Slug             string
	BodyMarkdown     string
	Status           Status
	FirstPublishedAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// New enforces creation invariants and returns an unpublished draft.
func New(orgID, projectID, categoryID uuid.UUID, title, slug, body string) (*Post, error) {
	title, slug, err := validate(title, slug, body, categoryID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Post{
		ID:           uuid.Must(uuid.NewV7()),
		OrgID:        orgID,
		ProjectID:    projectID,
		CategoryID:   categoryID,
		Title:        title,
		Slug:         slug,
		BodyMarkdown: body,
		Status:       StatusDraft,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// Update re-validates and replaces the editable fields, leaving status, first publication and tags alone.
func (p *Post) Update(title, slug, body string, categoryID uuid.UUID) error {
	title, slug, err := validate(title, slug, body, categoryID)
	if err != nil {
		return err
	}
	p.Title = title
	p.Slug = slug
	p.BodyMarkdown = body
	p.CategoryID = categoryID
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// Publish marks the post published, recording the first publication only once.
func (p *Post) Publish() {
	now := time.Now().UTC()
	p.Status = StatusPublished
	if p.FirstPublishedAt == nil {
		p.FirstPublishedAt = &now
	}
	p.UpdatedAt = now
}

// Unpublish returns the post to draft, retaining FirstPublishedAt.
func (p *Post) Unpublish() {
	p.Status = StatusDraft
	p.UpdatedAt = time.Now().UTC()
}

// SetTags replaces the tag set, de-duplicating and preserving first-seen order.
func (p *Post) SetTags(ids []uuid.UUID) {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	p.TagIDs = out
	p.UpdatedAt = time.Now().UTC()
}

// ListOpts filters Store.List. Zero value returns every post in scope.
type ListOpts struct {
	Status     *Status
	CategoryID *uuid.UUID
}

//nolint:gocritic // two same-typed strings; naming the results trips nonamedreturns instead.
func validate(title, slug, body string, categoryID uuid.UUID) (string, string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", "", &InvalidTitleError{Reason: "empty"}
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes {
		return "", "", &InvalidTitleError{Reason: "over 200 characters"}
	}
	if strings.TrimSpace(slug) == "" {
		slug = title
	}
	slug = slugify(slug)
	if slug == "" {
		return "", "", &InvalidSlugError{Reason: "derives to empty"}
	}
	if utf8.RuneCountInString(slug) > MaxSlugRunes {
		return "", "", &InvalidSlugError{Reason: "over 200 characters"}
	}
	if len(body) > MaxBodyBytes {
		return "", "", &InvalidBodyError{Reason: "over 64 KiB"}
	}
	if categoryID == uuid.Nil {
		return "", "", &CategoryRequiredError{}
	}
	return title, slug, nil
}

func slugify(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}
