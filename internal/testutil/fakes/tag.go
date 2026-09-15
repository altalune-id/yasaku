package fakes

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/blog/tag"
)

// Tag is an in-memory tag.Store.
type Tag struct {
	mu   sync.Mutex
	data map[uuid.UUID]*tag.Tag

	SaveFn   func(ctx context.Context, t *tag.Tag) error
	BySlugFn func(ctx context.Context, orgID, projectID uuid.UUID, slug string) (*tag.Tag, error)
	DeleteFn func(ctx context.Context, id uuid.UUID) error
}

// NewTag returns an empty in-memory tag.Store.
func NewTag() *Tag { return &Tag{data: map[uuid.UUID]*tag.Tag{}} }

var _ tag.Store = (*Tag)(nil)

// Seed inserts t without going through Save, bypassing the uniqueness check.
func (f *Tag) Seed(t *tag.Tag) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *t
	f.data[t.ID] = &cp
}

// Len reports how many tags are stored.
func (f *Tag) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.data)
}

func (f *Tag) Save(ctx context.Context, t *tag.Tag) error {
	if f.SaveFn != nil {
		return f.SaveFn(ctx, t)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, existing := range f.data {
		if id != t.ID && existing.ProjectID == t.ProjectID && existing.Slug == t.Slug {
			return &tag.AlreadyExistsError{Slug: t.Slug}
		}
	}
	cp := *t
	f.data[t.ID] = &cp
	return nil
}

func (f *Tag) ByID(_ context.Context, id uuid.UUID) (*tag.Tag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.data[id]
	if !ok {
		return nil, &tag.NotFoundError{ID: id.String()}
	}
	cp := *t
	return &cp, nil
}

func (f *Tag) ByIDs(_ context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*tag.Tag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[uuid.UUID]*tag.Tag, len(ids))
	for _, id := range ids {
		t, ok := f.data[id]
		if !ok || t.OrgID != orgID || t.ProjectID != projectID {
			continue
		}
		cp := *t
		out[id] = &cp
	}
	return out, nil
}

func (f *Tag) BySlug(ctx context.Context, orgID, projectID uuid.UUID, slug string) (*tag.Tag, error) {
	if f.BySlugFn != nil {
		return f.BySlugFn(ctx, orgID, projectID, slug)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.data {
		if t.OrgID == orgID && t.ProjectID == projectID && t.Slug == slug {
			cp := *t
			return &cp, nil
		}
	}
	return nil, &tag.NotFoundError{ID: slug}
}

func (f *Tag) List(_ context.Context, orgID, projectID uuid.UUID) ([]*tag.Tag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*tag.Tag, 0, len(f.data))
	for _, t := range f.data {
		if t.OrgID != orgID || t.ProjectID != projectID {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID.String() > out[j].ID.String()
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (f *Tag) Delete(ctx context.Context, id uuid.UUID) error {
	if f.DeleteFn != nil {
		return f.DeleteFn(ctx, id)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[id]; !ok {
		return &tag.NotFoundError{ID: id.String()}
	}
	delete(f.data, id)
	return nil
}
