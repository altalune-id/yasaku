package fakes

import (
	"context"
	"slices"
	"sync"

	"altalune.id/yasaku/internal/platform/queue"
)

// QueueCall is one recorded Queue.Submit.
type QueueCall struct {
	Job  queue.Job
	Data any
}

// Queue is an in-memory todo.Queue that records every Submit and returns Err.
type Queue struct {
	mu        sync.Mutex
	submitted []QueueCall
	Err       error
}

// Submit records the call and returns f.Err.
func (f *Queue) Submit(_ context.Context, j queue.Job, data any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitted = append(f.submitted, QueueCall{Job: j, Data: data})
	return f.Err
}

// Recorded returns a copy of the calls so far.
func (f *Queue) Recorded() []QueueCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.submitted)
}

// Reset clears the recorded submissions.
func (f *Queue) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitted = nil
}
