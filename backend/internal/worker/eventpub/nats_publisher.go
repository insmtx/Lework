// Package eventpub publishes fully constructed Worker business events.
package eventpub

import (
	"context"
	"fmt"
	"time"

	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

const terminalPublishTimeout = 5 * time.Second

// NATSEventPublisher publishes messaging.RunEvent through the appropriate NATS lane.
type NATSEventPublisher struct {
	bus eventbus.Publisher
}

// NewNATSEventPublisher creates a NATS-backed RunEvent publisher.
func NewNATSEventPublisher(bus eventbus.Publisher) *NATSEventPublisher {
	return &NATSEventPublisher{bus: bus}
}

// PublishRunEvent publishes a fully constructed Worker/Server business event.
func (p *NATSEventPublisher) PublishRunEvent(
	ctx context.Context,
	event messaging.RunEvent,
) error {
	if p == nil || p.bus == nil {
		return nil
	}
	if event.Body.Event == "" {
		return fmt.Errorf("run event type is required")
	}
	lane := messaging.ClassifyRunEvent(event.Body.Event)
	topic, err := messaging.RunEventSubject(event.Route.OrgID, event.Route.SessionID, lane)
	if err != nil {
		return fmt.Errorf("build run event subject: %w", err)
	}

	publishCtx := ctx
	publishCancel := func() {}
	if isTerminalRunEvent(event.Body.Event) {
		publishCtx, publishCancel = terminalPublishContext(ctx)
	}
	defer publishCancel()

	if err := p.bus.Publish(publishCtx, topic, event); err != nil {
		return fmt.Errorf("publish run event to %s: %w", topic, err)
	}
	return nil
}

func isTerminalRunEvent(eventType messaging.RunEventType) bool {
	return eventType == messaging.RunEventRunCompleted ||
		eventType == messaging.RunEventRunFailed ||
		eventType == messaging.RunEventRunCancelled
}

func terminalPublishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, terminalPublishTimeout)
}
