package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSerialObserverLimitsUnderlyingConcurrencyToOne(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	inner := NodeObserverFunc(func(_ context.Context, event NodeEvent) error {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		active.Add(-1)
		return nil
	})
	so := NewSerialObserver(inner)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := so.Observe(context.Background(), NodeEvent{Type: NodeEventMessageUpdate}); err != nil {
				t.Errorf("Observe() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if so.Err() != nil {
		t.Fatalf("SerialObserver recorded error: %v", so.Err())
	}
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("underlying observer max concurrency = %d, want 1", got)
	}
}

func TestSerialObserverPropagatesError(t *testing.T) {
	expected := context.Canceled
	inner := NodeObserverFunc(func(_ context.Context, event NodeEvent) error {
		return expected
	})
	so := NewSerialObserver(inner)

	err := so.Observe(context.Background(), NodeEvent{Type: "test"})
	if err != expected {
		t.Fatalf("first Observe error = %v, want %v", err, expected)
	}

	// Second observe should return the same error without calling the inner observer.
	err = so.Observe(context.Background(), NodeEvent{Type: "test2"})
	if err != expected {
		t.Fatalf("second Observe error = %v, want %v", err, expected)
	}
	if so.Err() != expected {
		t.Fatalf("Err() = %v, want %v", so.Err(), expected)
	}
}

func TestSerialObserverNilInner(t *testing.T) {
	so := NewSerialObserver(nil)
	if err := so.Observe(context.Background(), NodeEvent{Type: "test"}); err != nil {
		t.Fatalf("Observe with nil inner error = %v", err)
	}
}
