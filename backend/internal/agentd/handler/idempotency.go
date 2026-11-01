package handler

import (
	"sync"
	"time"
)

// idempotencyTTL bounds how long a prompt receipt is replayed. It is far
// longer than a reconnect window so a retried request after a dropped control
// connection still observes the original admission, while stale receipts do
// not accumulate for the lifetime of the process.
const idempotencyTTL = 10 * time.Minute

// idempotencyEntry is one cached prompt admission.
type idempotencyEntry struct {
	receipt   string
	expiresAt time.Time
}

// idempotencyStore deduplicates prompt admissions by caller-supplied key. It is
// mutex-protected because the runner handles requests and reconnect retries
// from independent goroutines.
type idempotencyStore struct {
	mu      sync.Mutex
	entries map[string]idempotencyEntry
	ttl     time.Duration
	now     func() time.Time
}

// newIdempotencyStore builds an empty store. now is a field so expiry can be
// tested without sleeping; production uses time.Now.
func newIdempotencyStore(ttl time.Duration) *idempotencyStore {
	if ttl <= 0 {
		ttl = idempotencyTTL
	}
	return &idempotencyStore{
		entries: make(map[string]idempotencyEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

// get returns a live receipt for key. Expired entries are reported as missing
// and dropped so a read cannot resurrect a stale admission.
func (s *idempotencyStore) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[key]
	if !ok {
		return "", false
	}
	if !s.now().Before(entry.expiresAt) {
		delete(s.entries, key)
		return "", false
	}
	return entry.receipt, true
}

// put records a receipt and lazily removes expired entries. Bucket cleanup is
// deferred to writes because that is the only point where the map grows.
func (s *idempotencyStore) put(key, receipt string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for k, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, k)
		}
	}
	s.entries[key] = idempotencyEntry{receipt: receipt, expiresAt: now.Add(s.ttl)}
}
