package runnable

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/ygpkg/yg-go/logs"
)

// StartSessionRunStarted subscribes to run.started events and marks source user messages as processing.
func StartSessionRunStarted(ictx context.Context, service contract.SessionService, eb eventbus.EventBus) {
	ctx := logs.WithContextFields(ictx, "runnable", "session_run_started")
	topic := dm.SessionResultStreamWildcardSubject()
	logs.InfoContextf(ctx, "starting session run started runnable: %s", topic)

	Run(ctx, "session_run_started", func(ctx context.Context) {
		if err := eb.Subscribe(ctx, topic, dm.SessionRunStartedConsumer(), func(msg *nats.Msg) {
			handleSessionRunStartedMessage(ctx, service, msg)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

func handleSessionRunStartedMessage(ctx context.Context, service contract.SessionService, msg *nats.Msg) {
	var streamMsg protocol.MessageStreamMessage
	if err := json.Unmarshal(msg.Data, &streamMsg); err != nil {
		logs.WarnContextf(ctx, "unmarshal session run started message: %v", err)
		return
	}
	if streamMsg.Body.Event != protocol.StreamEventRunStarted {
		return
	}
	sessionID := streamMsg.Route.SessionID
	if sessionID == "" {
		return
	}
	meta, err := msg.Metadata()
	if err != nil {
		logs.WarnContextf(ctx, "session run started missing nats metadata: session_id=%s error=%v", sessionID, err)
		return
	}
	if err := service.HandleSessionRunStarted(ctx, &contract.SessionRunStartedRequest{
		SessionID:         sessionID,
		ReplyToMessageIDs: streamMsg.Body.ReplyToMessageIDs,
		RequestID:         streamMsg.Trace.RequestID,
		StreamStartSeq:    meta.Sequence.Stream,
	}); err != nil {
		logs.WarnContextf(ctx, "handle session run started failed: session_id=%s error=%v", sessionID, err)
	}
}
