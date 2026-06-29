package events

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/insmtx/Leros/backend/agent"
)

// Emitter 填充通用事件元数据并将事件转发到 Sink。
type Emitter struct {
	runID   string
	traceID string
	sink    Sink
	seq     atomic.Int64
}

// NewEmitter 创建运行范围的事件发射器。
func NewEmitter(runID string, traceID string, sink Sink) *Emitter {
	if sink == nil {
		sink = NewNoopSink()
	}
	return &Emitter{
		runID:   runID,
		traceID: traceID,
		sink:    sink,
	}
}

// Emit 在填充稳定的元数据后转发一个事件。
func (e *Emitter) Emit(ctx context.Context, event *agent.Event) error {
	if e == nil || event == nil {
		return nil
	}
	event.Seq = e.seq.Add(1)
	if event.RunID == "" {
		event.RunID = e.runID
	}
	if event.TraceID == "" {
		event.TraceID = e.traceID
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.ID == "" {
		event.ID = fmt.Sprintf("%s:%d", event.RunID, event.Seq)
	}
	return e.sink.Emit(ctx, event)
}
