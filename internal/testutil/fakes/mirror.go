package fakes

import (
	"context"
	"slices"
	"sync"

	"altalune.id/yasaku/internal/platform/db"
)

// MirrorCall is one recorded Mark or Kick, with whether its ctx carried a unit of work.
type MirrorCall[R any] struct {
	Refs []R
	InTx bool
}

// Mirror is a recording mirror port for any module's ref type; a Kick with no refs is a no-op and is not recorded.
type Mirror[R any] struct {
	mu      sync.Mutex
	marks   []MirrorCall[R]
	kicks   []MirrorCall[R]
	MarkErr error
}

// Mark records the call and returns MarkErr.
func (m *Mirror[R]) Mark(ctx context.Context, refs ...R) error {
	_, inTx := db.CurrentTx(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.marks = append(m.marks, MirrorCall[R]{Refs: slices.Clone(refs), InTx: inTx})
	return m.MarkErr
}

// Kick records the call when it names any ref.
func (m *Mirror[R]) Kick(ctx context.Context, refs ...R) {
	if len(refs) == 0 {
		return
	}
	_, inTx := db.CurrentTx(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kicks = append(m.kicks, MirrorCall[R]{Refs: slices.Clone(refs), InTx: inTx})
}

// Marks returns a copy of the recorded Mark calls.
func (m *Mirror[R]) Marks() []MirrorCall[R] {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.marks)
}

// Kicks returns a copy of the recorded Kick calls.
func (m *Mirror[R]) Kicks() []MirrorCall[R] {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.kicks)
}
