package blog_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/blog"
)

func newTestPost(t *testing.T) *blog.Post {
	t.Helper()
	p, err := blog.New(uuid.New(), uuid.New(), uuid.New(), "First Post", "", "body")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func TestNew_Invariants(t *testing.T) {
	org, proj, cat := uuid.New(), uuid.New(), uuid.New()

	tests := []struct {
		name       string
		title      string
		slug       string
		body       string
		categoryID uuid.UUID
		wantTitle  string
		wantSlug   string
		wantErrIs  func(error) bool
	}{
		{name: "trims title and derives slug", title: "  Release Notes  ", categoryID: cat, wantTitle: "Release Notes", wantSlug: "release-notes"},
		{name: "explicit slug wins", title: "Release Notes", slug: "notes", categoryID: cat, wantTitle: "Release Notes", wantSlug: "notes"},
		{name: "collapses separators", title: "Go  &&  Rust", categoryID: cat, wantTitle: "Go  &&  Rust", wantSlug: "go-rust"},
		{name: "normalises an explicit slug", title: "Release Notes", slug: "  My Slug!!  ", categoryID: cat, wantTitle: "Release Notes", wantSlug: "my-slug"},
		{name: "accepts 200 rune title", title: strings.Repeat("a", 200), categoryID: cat, wantTitle: strings.Repeat("a", 200), wantSlug: strings.Repeat("a", 200)},
		{name: "accepts 64 KiB body", title: "Big", body: strings.Repeat("x", 64<<10), categoryID: cat, wantTitle: "Big", wantSlug: "big"},
		{name: "rejects empty title", title: "   ", categoryID: cat, wantErrIs: blog.IsInvalidTitleError},
		{name: "rejects over 200 rune title", title: strings.Repeat("a", 201), categoryID: cat, wantErrIs: blog.IsInvalidTitleError},
		{name: "rejects punctuation only title", title: "!!!", categoryID: cat, wantErrIs: blog.IsInvalidSlugError},
		{name: "rejects slug deriving to empty", title: "Release Notes", slug: "!!!", categoryID: cat, wantErrIs: blog.IsInvalidSlugError},
		{name: "rejects over 200 rune slug", title: "Release Notes", slug: strings.Repeat("a", 201), categoryID: cat, wantErrIs: blog.IsInvalidSlugError},
		{name: "rejects body over 64 KiB", title: "Big", body: strings.Repeat("x", (64<<10)+1), categoryID: cat, wantErrIs: blog.IsInvalidBodyError},
		{name: "rejects nil category", title: "Release Notes", categoryID: uuid.Nil, wantErrIs: blog.IsCategoryRequiredError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := blog.New(org, proj, tt.categoryID, tt.title, tt.slug, tt.body)
			if tt.wantErrIs != nil {
				if err == nil {
					t.Fatalf("New(%q, %q) = nil error, want one", tt.title, tt.slug)
				}
				if !tt.wantErrIs(err) {
					t.Fatalf("New(%q, %q) = %v, wrong error type", tt.title, tt.slug, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%q, %q) = %v", tt.title, tt.slug, err)
			}
			if p.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", p.Title, tt.wantTitle)
			}
			if p.Slug != tt.wantSlug {
				t.Errorf("Slug = %q, want %q", p.Slug, tt.wantSlug)
			}
			if p.OrgID != org || p.ProjectID != proj || p.CategoryID != tt.categoryID {
				t.Error("tenant scope or category not carried onto the aggregate")
			}
			if p.BodyMarkdown != tt.body {
				t.Error("body not carried onto the aggregate")
			}
			if p.ID == uuid.Nil {
				t.Error("New must assign an ID")
			}
			if p.Status != blog.StatusDraft || p.FirstPublishedAt != nil {
				t.Error("New must produce an unpublished draft")
			}
			if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() {
				t.Error("New must stamp CreatedAt and UpdatedAt")
			}
		})
	}
}

func TestPost_LifecycleIsIdempotentAndPreservesFirstPublish(t *testing.T) {
	p := newTestPost(t)
	if p.Status != blog.StatusDraft || p.FirstPublishedAt != nil {
		t.Fatal("a new post must start as an unpublished draft")
	}

	p.Publish()
	first := p.FirstPublishedAt
	if p.Status != blog.StatusPublished || first == nil {
		t.Fatal("Publish must set status and FirstPublishedAt")
	}

	p.Publish()
	if p.FirstPublishedAt != first {
		t.Error("re-publishing must not move FirstPublishedAt")
	}

	p.Unpublish()
	if p.Status != blog.StatusDraft {
		t.Error("Unpublish must return the post to draft")
	}
	if p.FirstPublishedAt != first {
		t.Error("Unpublish must retain FirstPublishedAt — it records that the post was once live")
	}

	p.Unpublish()
	if p.Status != blog.StatusDraft {
		t.Error("Unpublish on a draft must be a no-op, not an error state")
	}

	p.Publish()
	if p.FirstPublishedAt != first {
		t.Error("re-publishing after an unpublish must still preserve the original FirstPublishedAt")
	}
}

func TestPost_LifecycleBumpsUpdatedAt(t *testing.T) {
	p := newTestPost(t)
	p.UpdatedAt = p.UpdatedAt.Add(-time.Hour)

	before := p.UpdatedAt
	p.Publish()
	if !p.UpdatedAt.After(before) {
		t.Error("Publish must bump UpdatedAt")
	}

	before = p.UpdatedAt.Add(-time.Hour)
	p.UpdatedAt = before
	p.Unpublish()
	if !p.UpdatedAt.After(before) {
		t.Error("Unpublish must bump UpdatedAt")
	}
}

func TestPost_SetTagsDeduplicatesPreservingOrder(t *testing.T) {
	p := newTestPost(t)
	a, b := uuid.New(), uuid.New()
	p.SetTags([]uuid.UUID{a, b, a, b, a})
	if got := p.TagIDs; len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("SetTags = %v, want [%v %v]", got, a, b)
	}
}

func TestPost_SetTagsClearsTheSet(t *testing.T) {
	p := newTestPost(t)
	p.SetTags([]uuid.UUID{uuid.New()})
	p.SetTags(nil)
	if len(p.TagIDs) != 0 {
		t.Fatalf("SetTags(nil) = %v, want empty", p.TagIDs)
	}
}

func TestPost_Update(t *testing.T) {
	t.Run("revalidates and replaces every field", func(t *testing.T) {
		p := newTestPost(t)
		p.Publish()
		p.SetTags([]uuid.UUID{uuid.New()})
		wantStatus, wantFirst, wantTags := p.Status, p.FirstPublishedAt, p.TagIDs
		p.UpdatedAt = p.UpdatedAt.Add(-time.Hour)
		before := p.UpdatedAt
		cat := uuid.New()

		if err := p.Update("  Second Post  ", "", "new body", cat); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if p.Title != "Second Post" || p.Slug != "second-post" || p.BodyMarkdown != "new body" {
			t.Errorf("Update left the post as %+v", p)
		}
		if p.CategoryID != cat {
			t.Error("Update must move the post to the new category")
		}
		if !p.UpdatedAt.After(before) {
			t.Error("Update must bump UpdatedAt")
		}
		if p.Status != wantStatus || p.FirstPublishedAt != wantFirst {
			t.Error("Update must not touch Status or FirstPublishedAt")
		}
		if len(p.TagIDs) != len(wantTags) || p.TagIDs[0] != wantTags[0] {
			t.Error("Update must not touch TagIDs")
		}
	})

	t.Run("rejects invalid input without mutating", func(t *testing.T) {
		cat := uuid.New()
		tests := []struct {
			name       string
			title      string
			slug       string
			body       string
			categoryID uuid.UUID
			wantErrIs  func(error) bool
		}{
			{name: "empty title", title: "  ", categoryID: cat, wantErrIs: blog.IsInvalidTitleError},
			{name: "over long title", title: strings.Repeat("a", 201), categoryID: cat, wantErrIs: blog.IsInvalidTitleError},
			{name: "slug derives to empty", title: "ok", slug: "???", categoryID: cat, wantErrIs: blog.IsInvalidSlugError},
			{name: "over long body", title: "ok", body: strings.Repeat("x", (64<<10)+1), categoryID: cat, wantErrIs: blog.IsInvalidBodyError},
			{name: "nil category", title: "ok", categoryID: uuid.Nil, wantErrIs: blog.IsCategoryRequiredError},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				p := newTestPost(t)
				snapshot := *p
				err := p.Update(tt.title, tt.slug, tt.body, tt.categoryID)
				if err == nil {
					t.Fatal("Update = nil error, want one")
				}
				if !tt.wantErrIs(err) {
					t.Fatalf("Update = %v, wrong error type", err)
				}
				if p.Title != snapshot.Title || p.Slug != snapshot.Slug || p.BodyMarkdown != snapshot.BodyMarkdown ||
					p.CategoryID != snapshot.CategoryID || !p.UpdatedAt.Equal(snapshot.UpdatedAt) {
					t.Error("a rejected Update must leave the post untouched")
				}
			})
		}
	})
}

func TestErrors_CarryStableCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
		wantIs   func(error) bool
	}{
		{"not found", &blog.NotFoundError{ID: uuid.New().String()}, apperror.CodePostNotFound, blog.IsNotFoundError},
		{"already exists", &blog.AlreadyExistsError{Slug: "dup"}, apperror.CodePostAlreadyExists, blog.IsAlreadyExistsError},
		{"invalid title", &blog.InvalidTitleError{Reason: "empty"}, apperror.CodePostInvalidTitle, blog.IsInvalidTitleError},
		{"invalid slug", &blog.InvalidSlugError{Reason: "empty"}, apperror.CodePostInvalidSlug, blog.IsInvalidSlugError},
		{"invalid body", &blog.InvalidBodyError{Reason: "too large"}, apperror.CodePostInvalidBody, blog.IsInvalidBodyError},
		{"category required", &blog.CategoryRequiredError{}, apperror.CodePostCategoryRequired, blog.IsCategoryRequiredError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() == "" {
				t.Error("Error() must not be empty")
			}
			if !strings.HasPrefix(tt.err.Error(), "blog: ") {
				t.Errorf("Error() = %q, want a \"blog: \" prefix", tt.err.Error())
			}
			ae := tt.err.(interface{ ToAppError() *apperror.AppError }).ToAppError()
			if ae.Code() != tt.wantCode {
				t.Errorf("ToAppError().Code() = %q, want %q", ae.Code(), tt.wantCode)
			}
			if !tt.wantIs(fmt.Errorf("wrapped: %w", tt.err)) {
				t.Error("the Is helper must see through a wrap")
			}
			if tt.wantIs(errors.New("unrelated")) {
				t.Error("the Is helper must reject an unrelated error")
			}
		})
	}
}
