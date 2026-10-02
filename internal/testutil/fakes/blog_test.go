package fakes_test

import (
	"testing"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/blog"
	"altalune.id/yasaku/internal/testutil/fakes"
)

// TestBlogSaveMatchesTheRealStoresVersionContract pins the fake to the version contract both real stores hold.
func TestBlogSaveMatchesTheRealStoresVersionContract(t *testing.T) {
	ctx := t.Context()
	orgID, projectID, categoryID := uuid.New(), uuid.New(), uuid.New()

	store := fakes.NewBlog()
	p, err := blog.New(orgID, projectID, categoryID, "Hello", "hello", "body")
	if err != nil {
		t.Fatalf("new post: %v", err)
	}
	if err = store.Save(ctx, p, 0); err != nil {
		t.Fatalf("create: %v", err)
	}
	assertStoredVersion(t, store, p.ID, 1)

	if err = store.Save(ctx, p, 1); err != nil {
		t.Fatalf("guarded update at version 1: %v", err)
	}
	assertStoredVersion(t, store, p.ID, 2)

	p.Version++
	if err = store.Save(ctx, p, 2); err != nil {
		t.Fatalf("guarded update at version 2: %v (the real stores accept this)", err)
	}
	assertStoredVersion(t, store, p.ID, 3)
}

func TestBlogSaveRejectsAStaleVersion(t *testing.T) {
	ctx := t.Context()
	store := fakes.NewBlog()
	p, err := blog.New(uuid.New(), uuid.New(), uuid.New(), "Hello", "hello", "body")
	if err != nil {
		t.Fatalf("new post: %v", err)
	}
	if err = store.Save(ctx, p, 0); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err = store.Save(ctx, p, 7); !blog.IsStaleVersionError(err) {
		t.Fatalf("err = %v, want a *blog.StaleVersionError", err)
	}
	assertStoredVersion(t, store, p.ID, 1)
}

func assertStoredVersion(t *testing.T, store *fakes.Blog, id uuid.UUID, want int) {
	t.Helper()
	got, err := store.ByID(t.Context(), id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Version != want {
		t.Fatalf("stored version = %d, want %d", got.Version, want)
	}
}
