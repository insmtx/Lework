package events

import (
	"context"

	"github.com/insmtx/Leros/backend/agent"
)

// Sink 接收执行期间发出的运行时事件。
type Sink interface {
	Emit(ctx context.Context, event *agent.Event) error
}

// SinkFunc 将函数适配为 Sink。
type SinkFunc func(ctx context.Context, event *agent.Event) error

// Emit 调用 f(ctx, event)。
func (f SinkFunc) Emit(ctx context.Context, event *agent.Event) error {
	return f(ctx, event)
}

type noopSink struct{}

// NewNoopSink 返回一个丢弃所有事件的接收器。
func NewNoopSink() Sink {
	return noopSink{}
}

func (noopSink) Emit(context.Context, *agent.Event) error {
	return nil
}

// ChannelSink 将事件发送到通道，适用于 SSE/WebSocket 适配器。
type ChannelSink struct {
	C chan<- *agent.Event
}

// Emit 发送事件，不会因断开连接的消费者而永久阻塞。
func (s ChannelSink) Emit(ctx context.Context, event *agent.Event) error {
	if s.C == nil {
		return nil
	}
	select {
	case s.C <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
