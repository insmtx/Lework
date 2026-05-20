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
		if err := eb.Subscribe(ctx, topic, dm.SessionCompletedConsumer(), func(msg *nats.Msg) {
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
		completed := streamMsg.Body.RunCompleted
		if completed == nil {
			logs.WarnContextf(ctx, "run completed message missing run_completed payload: session_id=%s seq=%d", sessionID, streamMsg.Body.Seq)
			return
		}
		req := &contract.CompleteSessionMessageRequest{
			SessionID: sessionID,
			Content:   completed.Result.Message,
			Chunks:    runEventChunks(completed.Events),
			Metadata:  messageMetadataFromRunCompleted(completed),
			Usage:     messageUsageFromRuntime(completed.Usage),
			Seq:       streamMsg.Body.Seq,
			CreatedAt: streamMsg.CreatedAt,
		}
		if err := service.CompleteSessionMessage(ctx, req); err != nil {
			logs.WarnContextf(ctx, "complete session message: %v", err)
		}

	case events.StreamEventRunFailed:
		errMsg := streamMsg.Body.Payload.Content
		status := string(types.MessageStatusFailed)
		if streamMsg.Body.RunCompleted != nil && streamMsg.Body.RunCompleted.Result.Message != "" {
			errMsg = streamMsg.Body.RunCompleted.Result.Message
			if streamMsg.Body.RunCompleted.Status == string(types.MessageStatusCancelled) {
				status = string(types.MessageStatusCancelled)
			}
		}
		if streamMsg.Body.Error != nil {
			errMsg = streamMsg.Body.Error.Message
		}
		req := &contract.FailedSessionMessageRequest{
			SessionID: sessionID,
			Content:   errMsg,
			ErrorMsg:  errMsg,
			Status:    status,
			Chunks:    runEventChunks(streamMsg.Body.RunCompleted.Events),
			Metadata:  messageMetadataFromRunCompleted(streamMsg.Body.RunCompleted),
			Usage:     messageUsageFromRuntime(streamMsg.Body.RunCompleted.Usage),
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

func runEventChunks(records []events.RunEventRecord) []types.MessageChunk {
	if len(records) == 0 {
		return nil
	}
	chunks := make([]types.MessageChunk, 0, len(records))
	for _, record := range records {
		chunks = append(chunks, types.MessageChunk{
			Seq:       record.Seq,
			LastSeq:   record.LastSeq,
			Type:      string(record.Type),
			Timestamp: record.Timestamp,
			Payload:   append([]byte(nil), record.Payload...),
		})
	}
	return chunks
}

func messageUsageFromRuntime(usage *events.UsagePayload) *types.MessageUsage {
	if usage == nil {
		return nil
	}
	return &types.MessageUsage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
	}
}

func messageMetadataFromRunCompleted(completed *events.RunCompletedPayload) *types.MessageMetadata {
	if completed == nil {
		return nil
	}
	src := completed.Metadata
	if src == nil {
		return nil
	}

	// 直接序列化为 MessageMetadata，不匹配的字段会被忽略
	data, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	msgMetadata := &types.MessageMetadata{}
	if err := json.Unmarshal(data, msgMetadata); err != nil {
		return nil
	}
	return msgMetadata
}
