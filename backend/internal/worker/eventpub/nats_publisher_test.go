package eventpub

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type publisherRecorder struct {
	contextErr error
	topic      string
	event      any
	err        error
}

func (p *publisherRecorder) Publish(ctx context.Context, topic string, event any) error {
	p.contextErr = ctx.Err()
	p.topic = topic
	p.event = event
	return p.err
}

func (*publisherRecorder) Request(context.Context, string, any) (*nats.Msg, error) {
	return nil, nil
}

func TestNATSEventPublisherRoutesRunEventLane(t *testing.T) {
	recorder := &publisherRecorder{}
	publisher := NewNATSEventPublisher(recorder)
	event := messaging.RunEvent{
		ID:   "run-1:2",
		Type: messaging.MessageTypeRunEvent,
		Route: messaging.RouteContext{
			OrgID: 1, WorkerID: 2, SessionID: "session-1",
		},
		Body: messaging.RunEventBody{
			Seq:   2,
			Event: messaging.RunEventToolCallStarted,
		},
	}
	if err := publisher.PublishRunEvent(context.Background(), event); err != nil {
		t.Fatalf("PublishRunEvent() error = %v", err)
	}
	if !strings.Contains(recorder.topic, ".run.stream") {
		t.Fatalf("topic = %q, want stream lane", recorder.topic)
	}
	if got, ok := recorder.event.(messaging.RunEvent); !ok || got.ID != event.ID {
		t.Fatalf("published event = %#v", recorder.event)
	}
}

func TestNATSEventPublisherDetachesTerminalCancellation(t *testing.T) {
	recorder := &publisherRecorder{}
	publisher := NewNATSEventPublisher(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := publisher.PublishRunEvent(ctx, messaging.RunEvent{
		ID:   "run-1:3",
		Type: messaging.MessageTypeRunEvent,
		Route: messaging.RouteContext{
			OrgID: 1, SessionID: "session-1",
		},
		Body: messaging.RunEventBody{
			Seq:   3,
			Event: messaging.RunEventRunCancelled,
		},
	})
	if err != nil {
		t.Fatalf("PublishRunEvent() error = %v", err)
	}
	if recorder.contextErr != nil {
		t.Fatalf("terminal publish context error = %v", recorder.contextErr)
	}
}

func TestNATSEventPublisherPropagatesPublishError(t *testing.T) {
	expected := errors.New("publish failed")
	publisher := NewNATSEventPublisher(&publisherRecorder{err: expected})
	err := publisher.PublishRunEvent(context.Background(), messaging.RunEvent{
		Route: messaging.RouteContext{OrgID: 1, SessionID: "session-1"},
		Body:  messaging.RunEventBody{Event: messaging.RunEventRunStarted},
	})
	if !errors.Is(err, expected) {
		t.Fatalf("PublishRunEvent() error = %v, want %v", err, expected)
	}
}
