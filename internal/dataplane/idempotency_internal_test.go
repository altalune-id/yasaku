package dataplane

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newClockedStore(t *testing.T, ttl time.Duration, limit int, at *time.Time) *idempotencyStore {
	t.Helper()
	s := newIdempotencyStore(ttl, limit)
	s.now = func() time.Time { return *at }
	return s
}

func TestIdempotencyStoreExpiresARecordAfterTheTTL(t *testing.T) {
	projectID := uuid.New()
	now := time.Unix(1_700_000_000, 0).UTC()
	s := newClockedStore(t, time.Hour, IdempotencyMaxEntries, &now)
	body := []byte(`{"slug":"a"}`)

	if _, err := s.reserve(projectID, "k", body); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	s.store(projectID, "k", body, idempotencyResponse{status: http.StatusCreated, etag: `W/"1"`, body: []byte(`{"slug":"a"}`)})

	rec, err := s.reserve(projectID, "k", body)
	if err != nil || rec == nil {
		t.Fatalf("inside the TTL: rec = %v, err = %v, want the stored response", rec, err)
	}

	now = now.Add(2 * time.Hour)
	rec, err = s.reserve(projectID, "k", body)
	if err != nil {
		t.Fatalf("after the TTL: %v", err)
	}
	if rec != nil {
		t.Fatalf("an expired record was replayed: %+v", rec)
	}
	if got := s.len(); got != 1 {
		t.Fatalf("rows = %d, want 1 (the fresh reservation, the expired row swept)", got)
	}
}

func TestIdempotencyStoreIsBounded(t *testing.T) {
	projectID := uuid.New()
	now := time.Unix(1_700_000_000, 0).UTC()
	const limit = 8
	s := newClockedStore(t, time.Hour, limit, &now)

	keys := make([]string, 0, limit*4)
	for i := range limit * 4 {
		key := "key-" + uuid.New().String() + "-" + string(rune('a'+i%26))
		keys = append(keys, key)
		body := []byte(key)
		if _, err := s.reserve(projectID, key, body); err != nil {
			t.Fatalf("reserve %s: %v", key, err)
		}
		s.store(projectID, key, body, idempotencyResponse{status: http.StatusCreated, etag: `W/"1"`, body: make([]byte, 1024)})
		if got := s.len(); got > limit {
			t.Fatalf("after %d keys the store holds %d records, want at most %d", i+1, got, limit)
		}
	}

	oldest := keys[0]
	rec, err := s.reserve(projectID, oldest, []byte(oldest))
	if err != nil {
		t.Fatalf("reserve evicted key: %v", err)
	}
	if rec != nil {
		t.Fatalf("the oldest key survived %d later keys under a cap of %d", len(keys), limit)
	}
}

// SECURITY: two concurrent requests sharing one key must not both create.
func TestIdempotencyReservationExcludesAConcurrentDuplicate(t *testing.T) {
	projectID := uuid.New()
	now := time.Unix(1_700_000_000, 0).UTC()
	s := newClockedStore(t, time.Hour, IdempotencyMaxEntries, &now)
	body := []byte(`{"slug":"a"}`)

	rec, err := s.reserve(projectID, "k", body)
	if rec != nil || err != nil {
		t.Fatalf("first reserve = (%v, %v), want a fresh reservation", rec, err)
	}

	rec, err = s.reserve(projectID, "k", body)
	if rec != nil {
		t.Fatalf("a concurrent duplicate was handed a response that does not exist yet: %+v", rec)
	}
	if !IsInProgressError(err) {
		t.Fatalf("concurrent duplicate err = %v, want an *InProgressError", err)
	}

	if _, err = s.reserve(projectID, "k", []byte(`{"slug":"b"}`)); !IsConflictError(err) {
		t.Fatalf("a differing body under a held key = %v, want a *ConflictError", err)
	}

	s.store(projectID, "k", body, idempotencyResponse{status: http.StatusCreated, etag: `W/"1"`, body: []byte(`{"slug":"a"}`)})
	rec, err = s.reserve(projectID, "k", body)
	if err != nil || rec == nil || rec.resp.status != http.StatusCreated {
		t.Fatalf("replay after completion = (%v, %v), want the stored response", rec, err)
	}
}

func TestIdempotencyReleaseFreesAnUnfinishedKey(t *testing.T) {
	projectID := uuid.New()
	now := time.Unix(1_700_000_000, 0).UTC()
	s := newClockedStore(t, time.Hour, IdempotencyMaxEntries, &now)
	body := []byte(`{"slug":"a"}`)

	if _, err := s.reserve(projectID, "k", body); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	s.release(projectID, "k")
	if got := s.len(); got != 0 {
		t.Fatalf("rows = %d, want 0 after releasing the only reservation", got)
	}

	if _, err := s.reserve(projectID, "k", body); err != nil {
		t.Fatalf("reserve after release: %v, want the key to be free again", err)
	}
	s.store(projectID, "k", body, idempotencyResponse{status: http.StatusCreated, etag: `W/"1"`, body: []byte(`{"slug":"a"}`)})
	s.release(projectID, "k")

	rec, err := s.reserve(projectID, "k", body)
	if err != nil || rec == nil {
		t.Fatalf("release dropped a completed record: rec = %v, err = %v", rec, err)
	}
}
