package runnable

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

// StartSessionRunStreamProjector subscribes to the run.stream lane and records
// the NATS stream sequence of the first stream event for each session so that
// SSE replay knows where to start.
func StartSessionRunStreamProjector(ictx context.Context, service contract.SessionService, eb eventbus.EventBus) {
	ctx := logs.WithContextFields(ictx, "runnable", "session_run_stream_projector")
	topic := messaging.RunEventStreamWildcard()
	logs.InfoContextf(ctx, "starting session run stream projector: %s", topic)

	Run(ctx, "session_run_stream_projector", func(ctx context.Context) {
		if err := eb.Subscribe(ctx, topic, "", func(msg *nats.Msg) {
			handleRunStreamMessage(ctx, service, msg)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

var (
	streamSeenMu sync.Mutex
	streamSeen   = map[string]struct{}{}
)

func handleRunStreamMessage(ctx context.Context, service contract.SessionService, msg *nats.Msg) {
	var runEvent messaging.RunEvent
	if err := json.Unmarshal(msg.Data, &runEvent); err != nil {
		return
	}
	if runEvent.Type != messaging.MessageTypeRunEvent {
		return
	}

	sessionPID := runEvent.Route.SessionID
	if sessionPID == "" {
		return
	}

	// Set stream start seq at most once per session per process lifetime.
	streamSeenMu.Lock()
	if _, ok := streamSeen[sessionPID]; ok {
		streamSeenMu.Unlock()
		return
	}
	streamSeen[sessionPID] = struct{}{}
	streamSeenMu.Unlock()

	meta, err := msg.Metadata()
	if err != nil {
		logs.WarnContextf(ctx, "stream event missing nats metadata: session_id=%s error=%v", sessionPID, err)
		return
	}

	if err := service.SetSessionStreamStartSeq(ctx, sessionPID, meta.Sequence.Stream); err != nil {
		logs.WarnContextf(ctx, "set stream start seq failed: session_id=%s seq=%d error=%v",
			sessionPID, meta.Sequence.Stream, err)
		return
	}

	logs.DebugContextf(ctx, "recorded stream start seq: session_id=%s seq=%d", sessionPID, meta.Sequence.Stream)
}
