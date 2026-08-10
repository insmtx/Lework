// Package eventpub publishes fully constructed Worker business events.
package eventpub

import (
	"context"
	"fmt"
	"time"

	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

const publishTimeout = 5 * time.Second

// NATSEventPublisher publishes messaging.RunEvent through the appropriate NATS lane.
type NATSEventPublisher struct {
	bus eventbus.Publisher
}

// NewNATSEventPublisher creates a NATS-backed RunEvent publisher.
func NewNATSEventPublisher(bus eventbus.Publisher) *NATSEventPublisher {
	return &NATSEventPublisher{bus: bus}
}

// PublishRunEvent publishes a fully constructed Worker/Server business event.
//
// 上下文策略：
//   - 普通 stream/state 事件：从调用方 ctx 派生 + 5s 上限，遵守调用方取消；
//     调用方 ctx 取消时立即中止，不再等待最多 5 秒。
//   - 终态事件（completed/failed/cancelled）：脱离调用方取消，使用独立的 5s 短超时，
//     保证 Worker 关闭/运行取消时终态事件仍能尽力送达。
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

	publishCtx, publishCancel := buildPublishContext(ctx, event.Body.Event)
	defer publishCancel()

	if err := p.bus.Publish(publishCtx, topic, event); err != nil {
		return fmt.Errorf("publish run event to %s: %w", topic, err)
	}
	if lane == messaging.RunEventLaneState {
		logs.InfoContextf(publishCtx, "published run event (state): type=%s topic=%s session_id=%s run_id=%s",
			event.Body.Event, topic, event.Route.SessionID, event.Trace.RunID)
	} else {
		logs.DebugContextf(publishCtx, "published run event (stream): type=%s topic=%s session_id=%s run_id=%s",
			event.Body.Event, topic, event.Route.SessionID, event.Trace.RunID)
	}
	return nil
}

// buildPublishContext 按事件类型构造发布上下文。
func buildPublishContext(ctx context.Context, eventType messaging.RunEventType) (context.Context, context.CancelFunc) {
	if isTerminalRunEvent(eventType) {
		// 终态事件脱离调用方取消，独立 5s 短超时。
		return terminalPublishContext(ctx)
	}
	// 普通事件遵守调用方取消，仅叠加 5s 上限。
	base := context.Background()
	if ctx != nil {
		base = ctx
	}
	return context.WithTimeout(base, publishTimeout)
}

func terminalPublishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, publishTimeout)
}

// isTerminalRunEvent 判断是否为终态事件。
func isTerminalRunEvent(eventType messaging.RunEventType) bool {
	return eventType == messaging.RunEventRunCompleted ||
		eventType == messaging.RunEventRunFailed ||
		eventType == messaging.RunEventRunCancelled
}
