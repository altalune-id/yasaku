package fakes

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
)

// AuditRecord is one event the fake Audit saw, with whether it came inside a unit of work.
type AuditRecord struct {
	audit.Event
	OrgID uuid.UUID
	InTx  bool
}

// Audit is an in-memory audit recorder that, like the real one, refuses a Record outside a unit of work.
type Audit struct {
	mu      sync.Mutex
	records []AuditRecord
}

// Record keeps e when ctx carries a tenant scope and a unit of work.
func (a *Audit) Record(ctx context.Context, e audit.Event) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	_, inTx := db.CurrentTx(ctx)
	if !inTx {
		return &audit.NoUnitOfWorkError{Action: e.Action}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, AuditRecord{Event: e, OrgID: tc.OrgID, InTx: inTx})
	return nil
}

// Records returns a copy of every recorded event.
func (a *Audit) Records() []AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AuditRecord(nil), a.records...)
}

// Actions returns the recorded actions in order.
func (a *Audit) Actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.records))
	for _, r := range a.records {
		out = append(out, r.Action)
	}
	return out
}
