package handler

import (
	"context"
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

// idempotencyCall is one in-flight admission shared by every concurrent caller
// holding the same idempotency key. done is closed after receipt/err are set,
// so waiters observe the leader's outcome without polling.
type idempotencyCall struct {
	done    chan struct{}
	receipt string
	err     error
}

// idempotencyStore deduplicates prompt admissions by caller-supplied key. Both
// completed receipts and in-flight admissions are tracked under one mutex:
// concurrent callers with the same key wait for a single Prompt call instead of
// racing into duplicate submissions.
type idempotencyStore struct {
	mu       sync.Mutex
	entries  map[string]idempotencyEntry
	inflight map[string]*idempotencyCall
	ttl      time.Duration
	now      func() time.Time
}

// newIdempotencyStore builds an empty store. now is a field so expiry can be
// tested without sleeping; production uses time.Now.
func newIdempotencyStore(ttl time.Duration) *idempotencyStore {
	if ttl <= 0 {
		ttl = idempotencyTTL
	}
	return &idempotencyStore{
		entries:  make(map[string]idempotencyEntry),
		inflight: make(map[string]*idempotencyCall),
		ttl:      ttl,
		now:      time.Now,
	}
}

// do returns the cached receipt for key, or runs admit exactly once when no
// live receipt exists. Concurrent callers with the same key wait for the
// leader and receive its receipt or error. Only successful receipts are cached:
// an error is shared with current waiters but a later retry calls admit again.
func (s *idempotencyStore) do(
	ctx context.Context,
	key string,
	admit func(context.Context) (string, error),
) (string, error) {
	s.mu.Lock()
	if receipt, ok := s.getLocked(key); ok {
		s.mu.Unlock()
		return receipt, nil
	}
	if call, ok := s.inflight[key]; ok {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.receipt, call.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	call := &idempotencyCall{done: make(chan struct{})}
	s.inflight[key] = call
	s.mu.Unlock()

	receipt, err := admit(ctx)

	s.mu.Lock()
	delete(s.inflight, key)
	if err == nil {
		s.putLocked(key, receipt)
	}
	call.receipt = receipt
	call.err = err
	s.mu.Unlock()
	close(call.done)

	return receipt, err
}

// get returns a live receipt for key. Expired entries are reported as missing
// and dropped so a read cannot resurrect a stale admission.
func (s *idempotencyStore) get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(key)
}

// getLocked implements get while the caller holds s.mu.
func (s *idempotencyStore) getLocked(key string) (string, bool) {
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
	s.putLocked(key, receipt)
}

// putLocked implements put while the caller holds s.mu.
func (s *idempotencyStore) putLocked(key, receipt string) {
	now := s.now()
	for k, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, k)
		}
	}
	s.entries[key] = idempotencyEntry{receipt: receipt, expiresAt: now.Add(s.ttl)}
}
