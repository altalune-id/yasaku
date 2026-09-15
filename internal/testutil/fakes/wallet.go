package fakes

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/wallet"
)

// Wallet is an in-memory wallet.Store.
type Wallet struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*wallet.Wallet

	SaveFn   func(ctx context.Context, w *wallet.Wallet) error
	ListFn   func(ctx context.Context, orgID, projectID uuid.UUID, opts wallet.ListOpts) ([]*wallet.Wallet, error)
	DeleteFn func(ctx context.Context, id uuid.UUID) error
}

// NewWallet returns an empty in-memory wallet.Store.
func NewWallet() *Wallet { return &Wallet{rows: make(map[uuid.UUID]*wallet.Wallet)} }

var _ wallet.Store = (*Wallet)(nil)

// Save upserts by ID, mirroring the partial unique index on (project_id, lower(name)) where archived_at is null.
func (r *Wallet) Save(ctx context.Context, w *wallet.Wallet) error {
	if r.SaveFn != nil {
		return r.SaveFn(ctx, w)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !w.IsArchived() {
		for id, existing := range r.rows {
			if id == w.ID || existing.IsArchived() || existing.ProjectID != w.ProjectID {
				continue
			}
			if strings.EqualFold(existing.Name, w.Name) {
				return &wallet.AlreadyExistsError{Name: w.Name}
			}
		}
	}
	cp := *w
	r.rows[w.ID] = &cp
	return nil
}

// ByID returns the wallet with the given ID. NOTE: deliberately unfiltered by project, so a scope test exercises the service's own guard.
func (r *Wallet) ByID(_ context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.rows[id]
	if !ok {
		return nil, &wallet.NotFoundError{ID: id.String()}
	}
	cp := *w
	return &cp, nil
}

// List returns the wallets in scope ordered by name then id.
func (r *Wallet) List(ctx context.Context, orgID, projectID uuid.UUID, opts wallet.ListOpts) ([]*wallet.Wallet, error) {
	if r.ListFn != nil {
		return r.ListFn(ctx, orgID, projectID, opts)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*wallet.Wallet, 0, len(r.rows))
	for _, w := range r.rows {
		if w.OrgID != orgID || w.ProjectID != projectID {
			continue
		}
		if w.IsArchived() && !opts.IncludeArchived {
			continue
		}
		cp := *w
		out = append(out, &cp)
	}
	slices.SortFunc(out, func(a, b *wallet.Wallet) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	return out, nil
}

// Delete removes the wallet or returns a *NotFoundError.
func (r *Wallet) Delete(ctx context.Context, id uuid.UUID) error {
	if r.DeleteFn != nil {
		return r.DeleteFn(ctx, id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.rows[id]; !ok {
		return &wallet.NotFoundError{ID: id.String()}
	}
	delete(r.rows, id)
	return nil
}
