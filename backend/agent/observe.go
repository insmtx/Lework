package agent

import (
	"context"
	"sync"
)

// SerialObserver wraps a NodeObserver to guarantee that all Observe calls
// are serialized. Concurrent calls from runtime activity (e.g. parallel
// tool completions) are enqueued and processed in order without blocking
// the runtime goroutine for longer than the serialization window.
//
// An error from the underlying observer terminates execution — subsequent
// observes are dropped and the first error is returned.
type SerialObserver struct {
	mu    sync.Mutex
	inner NodeObserver
	err   error
}

// NewSerialObserver wraps a NodeObserver for serial event delivery.
func NewSerialObserver(inner NodeObserver) *SerialObserver {
	return &SerialObserver{inner: inner}
}

// Observe serializes access to the underlying observer. After the first error,
// subsequent calls are dropped and return the stored error.
func (s *SerialObserver) Observe(ctx context.Context, event NodeEvent) error {
	if s == nil || s.inner == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if err := s.inner.Observe(ctx, event); err != nil {
		s.err = err
		return err
	}
	return nil
}

// Err returns the first observer error, if any.
func (s *SerialObserver) Err() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Inner returns the wrapped observer for inspection in tests.
func (s *SerialObserver) Inner() NodeObserver {
	if s == nil {
		return nil
	}
	return s.inner
}

var _ NodeObserver = (*SerialObserver)(nil)
