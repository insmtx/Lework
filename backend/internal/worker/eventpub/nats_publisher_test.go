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

// TestNATSEventPublisherNormalEventHonorsCallerCancellation 验证普通事件遵守调用方取消：
// 调用方 ctx 已取消时，普通事件应立即中止（ctx.Err()），不再脱离取消等待最多 5 秒。
func TestNATSEventPublisherNormalEventHonorsCallerCancellation(t *testing.T) {
	recorder := &publisherRecorder{}
	publisher := NewNATSEventPublisher(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 调用方已取消
	err := publisher.PublishRunEvent(ctx, messaging.RunEvent{
		ID:   "run-1:4",
		Type: messaging.MessageTypeRunEvent,
		Route: messaging.RouteContext{
			OrgID: 1, SessionID: "session-1",
		},
		Body: messaging.RunEventBody{Seq: 4, Event: messaging.RunEventMessageDelta},
	})
	// 普通事件：即使 ctx 已取消，publisher 仍会把 ctx 传给下层，由下层以 ctx.Err() 失败；
	// 这里验证下层收到的 ctx 是"已取消"的（而非 WithoutCancel 派生的无取消 ctx）。
	if recorder.contextErr != context.Canceled {
		t.Fatalf("normal publish context error = %v, want context.Canceled", recorder.contextErr)
	}
	if err == nil {
		// 真实场景下发布会因 ctx 取消失败；这里 recorder 不检查 ctx 仅记录，故 err 可能为 nil。
		t.Log("publish returned nil (recorder ignores ctx), but observed cancelled context propagated")
	}
}
