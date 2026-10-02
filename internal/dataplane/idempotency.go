package dataplane

import (
	"container/list"
	"crypto/sha256"
	"crypto/subtle"
	"sync"
	"time"

	"github.com/google/uuid"
)

// IdempotencyTTL is how long a replayed Idempotency-Key keeps returning its first response.
const IdempotencyTTL = 24 * time.Hour

// IdempotencyMaxEntries bounds how many keys one process remembers, evicting the oldest first.
const IdempotencyMaxEntries = 4096

type idempotencyResponse struct {
	status int
	etag   string
	body   []byte
}

type idempotencyKey struct {
	projectID uuid.UUID
	key       string
}

type idempotencyRecord struct {
	bodyHash [32]byte
	resp     idempotencyResponse
	expires  time.Time
	pending  bool
}

type idempotencyEntry struct {
	key idempotencyKey
	rec idempotencyRecord
}

// NOTE: process-local and best effort — not shared between replicas, not durable across a restart.
type idempotencyStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	limit int
	now   func() time.Time
	rows  map[idempotencyKey]*list.Element
	order *list.List
}

func newIdempotencyStore(ttl time.Duration, limit int) *idempotencyStore {
	return &idempotencyStore{
		ttl:   ttl,
		limit: limit,
		now:   func() time.Time { return time.Now().UTC() },
		rows:  map[idempotencyKey]*list.Element{},
		order: list.New(),
	}
}

// NOTE: on a claim this returns (nil, nil) and the caller must finish with store or release.
func (s *idempotencyStore) reserve(projectID uuid.UUID, key string, body []byte) (*idempotencyRecord, error) {
	sum := sha256.Sum256(body)
	k := idempotencyKey{projectID: projectID, key: key}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()

	if el, ok := s.rows[k]; ok {
		entry := entryOf(el)
		if subtle.ConstantTimeCompare(entry.rec.bodyHash[:], sum[:]) != 1 {
			return nil, &ConflictError{}
		}
		if entry.rec.pending {
			return nil, &InProgressError{}
		}
		rec := entry.rec
		return &rec, nil
	}

	s.insert(k, idempotencyRecord{bodyHash: sum, expires: s.now().Add(s.ttl), pending: true})
	return nil, nil //nolint:nilnil // a fresh reservation is neither a hit nor a failure.
}

func (s *idempotencyStore) store(projectID uuid.UUID, key string, body []byte, resp idempotencyResponse) {
	k := idempotencyKey{projectID: projectID, key: key}
	rec := idempotencyRecord{
		bodyHash: sha256.Sum256(body),
		resp:     resp,
		expires:  s.now().Add(s.ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.rows[k]; ok {
		entryOf(el).rec = rec
		s.order.MoveToBack(el)
		return
	}
	s.insert(k, rec)
}

func (s *idempotencyStore) release(projectID uuid.UUID, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.rows[idempotencyKey{projectID: projectID, key: key}]
	if !ok || !entryOf(el).rec.pending {
		return
	}
	s.remove(el)
}

func (s *idempotencyStore) insert(k idempotencyKey, rec idempotencyRecord) {
	s.rows[k] = s.order.PushBack(&idempotencyEntry{key: k, rec: rec})
	for s.limit > 0 && s.order.Len() > s.limit {
		s.remove(s.order.Front())
	}
}

func (s *idempotencyStore) remove(el *list.Element) {
	delete(s.rows, entryOf(el).key)
	s.order.Remove(el)
}

func (s *idempotencyStore) sweep() {
	now := s.now()
	for el := s.order.Front(); el != nil; el = s.order.Front() {
		if !now.After(entryOf(el).rec.expires) {
			return
		}
		s.remove(el)
	}
}

func (s *idempotencyStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func entryOf(el *list.Element) *idempotencyEntry {
	entry, ok := el.Value.(*idempotencyEntry)
	if !ok {
		panic("dataplane: idempotency order holds a foreign element")
	}
	return entry
}
