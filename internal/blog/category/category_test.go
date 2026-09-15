package category_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/blog/category"
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
			c, err := category.New(org, proj, tt.in, tt.slug)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("New(%q) = nil error, want one", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%q) = %v", tt.in, err)
			}
			if c.Slug != tt.wantSlug {
				t.Errorf("Slug = %q, want %q", c.Slug, tt.wantSlug)
			}
			if c.OrgID != org || c.ProjectID != proj {
				t.Error("tenant scope not carried onto the aggregate")
			}
		})
	}
}

func TestRename(t *testing.T) {
	org, proj := uuid.New(), uuid.New()
	c, err := category.New(org, proj, "Release Notes", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("keeps the slug stable", func(t *testing.T) {
		if err := c.Rename("  Changelog  "); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if c.Name != "Changelog" {
			t.Errorf("Name = %q, want %q", c.Name, "Changelog")
		}
		if c.Slug != "release-notes" {
			t.Errorf("Slug = %q, want it unchanged", c.Slug)
		}
	})
	t.Run("empty rejected", func(t *testing.T) {
		if err := c.Rename("   "); !category.IsInvalidNameError(err) {
			t.Fatalf("Rename(%q) = %v, want InvalidNameError", "   ", err)
		}
	})
	t.Run("over 100 runes rejected", func(t *testing.T) {
		if err := c.Rename(strings.Repeat("b", 101)); !category.IsInvalidNameError(err) {
			t.Fatalf("Rename(101 runes) = %v, want InvalidNameError", err)
		}
	})
}
