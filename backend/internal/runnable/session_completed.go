package runnable

import (
	"context"
	"encoding/json"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// StartSessionCompleted subscribes to session completed events and dispatches to the service.
func StartSessionCompleted(ictx context.Context, service contract.SessionService, eb eventbus.EventBus) {
	ctx := logs.WithContextFields(ictx, "runnable", "session_completed")
	topic := dm.SessionMessageCompletedWildcardSubject()
	logs.InfoContextf(ctx, "starting session completed runnable: %s", topic)

	Run(ctx, "session_completed", func(ctx context.Context) {
		if err := eb.SubscribeFrom(ctx, topic, 0, func(msg *nats.Msg) {
			handleSessionCompletedMessage(ctx, service, msg)
		}); err != nil {
			logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
		}
	})
}

func handleSessionCompletedMessage(ctx context.Context, service contract.SessionService, msg *nats.Msg) {
	var streamMsg events.MessageStreamMessage
	if err := json.Unmarshal(msg.Data, &streamMsg); err != nil {
		logs.WarnContextf(ctx, "unmarshal session completed message: %v", err)
		return
	}

	sessionID := streamMsg.Route.SessionID
	if sessionID == "" {
		return
	}

	switch streamMsg.Body.Event {
	case events.StreamEventRunCompleted:
		req := &contract.CompleteSessionMessageRequest{
			SessionID: sessionID,
			Content:   streamMsg.Body.Payload.Content,
			Seq:       streamMsg.Body.Seq,
			CreatedAt: streamMsg.CreatedAt,
		}
		if tc := streamMsg.Body.Payload.ToolCall; tc != nil {
			req.ToolCalls = []types.ToolCall{{
				ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments, Status: types.ToolCallStatusSuccess,
			}}
		}
		if u := streamMsg.Body.Usage; u != nil {
			req.Metadata = &types.MessageMetadata{Tokens: u.TotalTokens}
		}
		if err := service.CompleteSessionMessage(ctx, req); err != nil {
			logs.WarnContextf(ctx, "complete session message: %v", err)
		}

	case events.StreamEventRunFailed:
		errMsg := streamMsg.Body.Payload.Content
		if streamMsg.Body.Error != nil {
			errMsg = streamMsg.Body.Error.Message
		}
		req := &contract.FailedSessionMessageRequest{
			SessionID: sessionID,
			ErrorMsg:  errMsg,
			Seq:       streamMsg.Body.Seq,
			CreatedAt: streamMsg.CreatedAt,
		}
		if streamMsg.Body.Error != nil {
			req.ErrorCode = streamMsg.Body.Error.Code
		}
		if err := service.FailedSessionMessage(ctx, req); err != nil {
			logs.WarnContextf(ctx, "failed session message: %v", err)
		}

	default:
		logs.DebugContextf(ctx, "ignoring session completed event: %s", streamMsg.Body.Event)
	}
}
