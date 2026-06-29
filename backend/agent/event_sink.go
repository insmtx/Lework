package agent

import "context"

// EventSink receives observable events emitted during a run.
type EventSink interface {
	Emit(ctx context.Context, event *Event) error
}
