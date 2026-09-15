package tag_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/blog/tag"
)

func TestNew_Invariants(t *testing.T) {
	org, proj := uuid.New(), uuid.New()
	tests := []struct {
		name, in, slug string
		wantSlug       string
		wantErr        bool
	}{
		{"trims and derives slug", "  Release Notes  ", "", "release-notes", false},
		{"explicit slug wins", "Release Notes", "notes", "notes", false},
		{"empty name rejected", "   ", "", "", true},
		{"punctuation-only name rejected", "!!!", "", "", true},
		{"over 100 runes rejected", strings.Repeat("a", 101), "", "", true},
		{"exactly 100 runes allowed", strings.Repeat("a", 100), "", strings.Repeat("a", 100), false},
		{"collapses separators", "Go  &&  Rust", "", "go-rust", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg, err := tag.New(org, proj, tt.in, tt.slug)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("New(%q) = nil error, want one", tt.in)
				}
				if !tag.IsInvalidNameError(err) {
					t.Fatalf("New(%q) = %T, want *tag.InvalidNameError", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%q) = %v", tt.in, err)
			}
			if tg.Slug != tt.wantSlug {
				t.Errorf("Slug = %q, want %q", tg.Slug, tt.wantSlug)
			}
			if tg.OrgID != org || tg.ProjectID != proj {
				t.Error("tenant scope not carried onto the aggregate")
			}
		})
	}
}

func TestNew_SlugIsStableAcrossNameCasing(t *testing.T) {
	org, proj := uuid.New(), uuid.New()
	a, err := tag.New(org, proj, "Go Lang", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := tag.New(org, proj, "go lang", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Slug != b.Slug {
		t.Fatalf("slugs differ: %q vs %q — EnsureByName would create a duplicate", a.Slug, b.Slug)
	}
}

func TestNew_TrimsNameAndStampsTimestamps(t *testing.T) {
	tg, err := tag.New(uuid.New(), uuid.New(), "  Go  ", "")
	if err != nil {
		t.Fatal(err)
	}
	if tg.Name != "Go" {
		t.Errorf("Name = %q, want %q", tg.Name, "Go")
	}
	if tg.ID == uuid.Nil {
		t.Error("ID not assigned")
	}
	if tg.CreatedAt.IsZero() || tg.UpdatedAt.IsZero() {
		t.Error("timestamps not stamped")
	}
}

func TestRename(t *testing.T) {
	tg, err := tag.New(uuid.New(), uuid.New(), "Go Lang", "")
	if err != nil {
		t.Fatal(err)
	}
	before := tg.UpdatedAt
	slug := tg.Slug

	if err := tg.Rename("  Golang  "); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if tg.Name != "Golang" {
		t.Errorf("Name = %q, want %q", tg.Name, "Golang")
	}
	if tg.Slug != slug {
		t.Errorf("Rename changed the slug: %q -> %q; links would break", slug, tg.Slug)
	}
	if tg.UpdatedAt.Before(before) {
		t.Error("UpdatedAt went backwards")
	}

	if err := tg.Rename("   "); !tag.IsInvalidNameError(err) {
		t.Errorf("Rename(blank) = %v, want *tag.InvalidNameError", err)
	}
	if err := tg.Rename(strings.Repeat("a", 101)); !tag.IsInvalidNameError(err) {
		t.Errorf("Rename(101 runes) = %v, want *tag.InvalidNameError", err)
	}
	if tg.Name != "Golang" {
		t.Errorf("a rejected Rename mutated the aggregate: Name = %q", tg.Name)
	}
}
