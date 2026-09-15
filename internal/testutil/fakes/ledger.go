package fakes

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/ledger"
)

// Ledger is an in-memory ledger.Store.
type Ledger struct {
	mu   sync.Mutex
	data map[uuid.UUID]*ledger.Settings
}

// NewLedger returns an empty in-memory ledger.Store.
func NewLedger() *Ledger { return &Ledger{data: map[uuid.UUID]*ledger.Settings{}} }

var _ ledger.Store = (*Ledger)(nil)

// Seed stores s without going through Save.
func (f *Ledger) Seed(s *ledger.Settings) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *s
	f.data[s.ProjectID] = &cp
}

// Len reports how many projects have stored settings.
func (f *Ledger) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.data)
}

// Save stores a copy of s, keyed by project.
func (f *Ledger) Save(_ context.Context, s *ledger.Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *s
	f.data[s.ProjectID] = &cp
	return nil
}

// ByProject returns a copy of the project's settings, or a *ledger.NotFoundError.
func (f *Ledger) ByProject(_ context.Context, orgID, projectID uuid.UUID) (*ledger.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.data[projectID]
	if !ok || s.OrgID != orgID {
		return nil, &ledger.NotFoundError{ProjectID: projectID.String()}
	}
	cp := *s
	return &cp, nil
}
