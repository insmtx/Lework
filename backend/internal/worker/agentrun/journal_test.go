package agentrun

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type concurrencyTrackingPublisher struct {
	active    atomic.Int32
	maxActive atomic.Int32

	mu   sync.Mutex
	seqs []int64
}

func (p *concurrencyTrackingPublisher) PublishRunEvent(
	_ context.Context,
	event messaging.RunEvent,
) error {
	current := p.active.Add(1)
	for {
		previous := p.maxActive.Load()
		if current <= previous || p.maxActive.CompareAndSwap(previous, current) {
			break
		}
	}
	time.Sleep(2 * time.Millisecond)
	p.mu.Lock()
	p.seqs = append(p.seqs, event.Body.Seq)
	p.mu.Unlock()
	p.active.Add(-1)
	return nil
}

func TestJournalSerializesPublishOrderWithSeqAssignment(t *testing.T) {
	publisher := &concurrencyTrackingPublisher{}
	journal := NewJournal(
		&agentrundomain.RunRequest{RunID: "run-journal", TraceID: "trace-journal"},
		testEventContext("run-journal"),
		publisher,
	)

	const total = 12
	var wg sync.WaitGroup
	wg.Add(total)
	for i := 0; i < total; i++ {
		go func() {
			defer wg.Done()
			if err := journal.Record(context.Background(), RunEventDraft{
				Body: messaging.RunEventBody{
					Event: messaging.RunEventMessageDelta,
					Payload: messaging.RunEventPayload{
						MessageID: "m1",
						Role:      messaging.MessageRoleAssistant,
						Content:   "x",
					},
				},
			}); err != nil {
				t.Errorf("Record() error = %v", err)
			}
		}()
	}
	wg.Wait()

	if got := publisher.maxActive.Load(); got != 1 {
		t.Fatalf("publisher max concurrency = %d, want 1", got)
	}
	if len(publisher.seqs) != total {
		t.Fatalf("published seqs = %v, want %d events", publisher.seqs, total)
	}
	for index, seq := range publisher.seqs {
		want := int64(index + 1)
		if seq != want {
			t.Fatalf("published seqs = %v, want strict ascending order", publisher.seqs)
		}
	}
}
