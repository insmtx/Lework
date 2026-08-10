package agentrun

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type runJournal struct {
	mu           sync.Mutex
	runID        string
	traceID      string
	eventContext EventContext
	publisher    RunEventPublisher
	maxSeq       int64
	events       []messaging.RunEventRecord

	// publishMu 保护单个 Run 的事件发布串行化，避免并发发布打乱 seq 顺序。
	// 同时只串行化本 Run 的 NATS IO，不阻塞其它 Run 的并行发布。
	publishMu sync.Mutex
	// publishCond 按 seq 顺序放行发布：只有 nextPublishSeq 对应的事件才能发布，
	// 保证"按 seq 顺序发布"而非仅"不并发发布"。
	publishCond    *sync.Cond
	nextPublishSeq int64

	toolFailures int
	toolNames    []string
	messageCount int
	usage        *agentrundomain.Usage
	toolRecords  []agentrundomain.ToolCallRecord
}

// NewJournal creates a Journal bound to a request and an explicit publisher.
func NewJournal(
	req *agentrundomain.RunRequest,
	eventContext EventContext,
	publisher RunEventPublisher,
) Journal {
	journal := &runJournal{
		eventContext:   cloneEventContext(eventContext),
		publisher:      publisher,
		nextPublishSeq: 1, // 与 maxSeq 从 0 递增对齐：首个 seq 为 1
	}
	journal.publishCond = sync.NewCond(&journal.publishMu)
	if req != nil {
		journal.runID = req.RunID
		journal.traceID = req.TraceID
	}
	if journal.runID == "" {
		journal.runID = eventContext.RunID
	}
	if journal.traceID == "" {
		journal.traceID = eventContext.TraceID
	}
	return journal
}

func (j *runJournal) Record(ctx context.Context, draft RunEventDraft) error {
	if j == nil {
		return nil
	}
	body, err := cloneRunEventBody(draft.Body)
	if err != nil {
		return fmt.Errorf("clone run event body: %w", err)
	}
	occurredAt := draft.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	// 状态变更（分配 seq、归档、观察）在状态锁下完成；NATS IO 在锁外执行，
	// 避免持有状态锁进行外部发布阻塞同 Run 的其它状态更新。
	// 注意：归档失败时不得跳过发布闸门，否则后续 seq 会永久等待——必须保证每个
	// 已分配 seq 都完整经过闸门（归档是尽力而为的快照，失败不影响事件发布）。
	j.mu.Lock()
	j.maxSeq++
	body.Seq = j.maxSeq
	if len(body.ReplyToMessageIDs) == 0 {
		body.ReplyToMessageIDs = append([]string(nil), j.eventContext.ReplyToMessageIDs...)
	}
	j.observeLocked(body)
	var archiveErr error
	if !isTerminalRunEvent(body.Event) {
		record, recordErr := archiveRecord(body, occurredAt, j.eventContext.AssistantPublicID)
		if recordErr != nil {
			archiveErr = recordErr
		} else {
			j.events = append(j.events, record)
		}
	}
	event := j.envelopeLocked(body, occurredAt)
	publisher := j.publisher
	seq := body.Seq
	j.mu.Unlock()

	// 按 seq 顺序发布：获取发布锁后，必须等自己的 seq 轮到（nextPublishSeq）才能发布。
	// 这样不仅避免并发，更保证"低 seq 先发布"，杜绝乱序。锁内不做其它状态 IO，
	// 不阻塞同 Run 的其它状态更新，也不阻塞其它 Run 的并行发布。
	j.publishMu.Lock()
	for seq != j.nextPublishSeq {
		j.publishCond.Wait()
	}
	var publishErr error
	if publisher != nil {
		publishErr = publisher.PublishRunEvent(ctx, event)
	}
	// 无论是否实际发布（如无 publisher）、或归档失败，都推进闸门，避免后续 seq 永久等待。
	j.nextPublishSeq++
	j.publishCond.Broadcast()
	j.publishMu.Unlock()

	// 归档失败：返回归档错误，但事件已发布、闸门已推进，不影响后续事件。
	if archiveErr != nil {
		return fmt.Errorf("archive run event: %w", archiveErr)
	}
	return publishErr
}

func (j *runJournal) Snapshot() JournalSnapshot {
	if j == nil {
		return JournalSnapshot{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	var usage *agentrundomain.Usage
	if j.usage != nil {
		copied := *j.usage
		usage = &copied
	}
	return JournalSnapshot{
		ToolCalls:    append([]agentrundomain.ToolCallRecord(nil), j.toolRecords...),
		Usage:        usage,
		MessageCount: j.messageCount,
		ToolFailures: j.toolFailures,
		ToolNames:    append([]string(nil), j.toolNames...),
		Events:       mergeArchivedDeltas(cloneRunEventRecords(j.events)),
	}
}

func (j *runJournal) envelopeLocked(
	body messaging.RunEventBody,
	occurredAt time.Time,
) messaging.RunEvent {
	runID := j.runID
	if runID == "" {
		runID = j.eventContext.RunID
	}
	traceID := j.traceID
	if traceID == "" {
		traceID = j.eventContext.TraceID
	}
	body.AssistantPKID = j.eventContext.AssistantID
	body.AssistantID = j.eventContext.AssistantPublicID
	body.MemberCommandIDs = append([]string(nil), j.eventContext.MemberCommandIDs...)
	return messaging.RunEvent{
		ID:        fmt.Sprintf("%s:%d", runID, body.Seq),
		Type:      messaging.MessageTypeRunEvent,
		CreatedAt: occurredAt,
		Trace: messaging.TraceContext{
			TraceID:   traceID,
			RequestID: j.eventContext.RequestID,
			TaskID:    j.eventContext.TaskID,
			RunID:     runID,
			ParentID:  j.eventContext.ParentID,
		},
		Route: messaging.RouteContext{
			OrgID:             j.eventContext.OrgID,
			WorkerID:          j.eventContext.WorkerID,
			WorkerPublicID:    j.eventContext.WorkerPublicID,
			SessionID:         j.eventContext.SessionID,
			AssistantID:       j.eventContext.AssistantID,
			AssistantPublicID: j.eventContext.AssistantPublicID,
			ClientIP:          j.eventContext.ClientIP,
		},
		Body: body,
	}
}

func (j *runJournal) observeLocked(body messaging.RunEventBody) {
	switch body.Event {
	case messaging.RunEventMessageDelta:
		j.messageCount++
	case messaging.RunEventMessageCompleted:
		j.messageCount++
		if body.Payload.Usage != nil {
			j.usage = usageFromMessaging(body.Payload.Usage)
		}
	case messaging.RunEventToolCallStarted:
		if body.Payload.ToolCall != nil {
			name := strings.TrimSpace(body.Payload.ToolCall.Name)
			if name != "" {
				j.toolNames = append(j.toolNames, name)
			}
		}
	case messaging.RunEventToolCallFinished:
		if body.Payload.ToolResult == nil {
			return
		}
		result := body.Payload.ToolResult
		if result.IsError {
			j.toolFailures++
		}
		j.toolRecords = append(j.toolRecords, agentrundomain.ToolCallRecord{
			CallID: result.ToolCallID,
			Name:   result.Name,
			Result: append(json.RawMessage(nil), result.Result...),
			Error:  result.Error,
		})
	}
}

func archiveRecord(
	body messaging.RunEventBody,
	occurredAt time.Time,
	assistantID string,
) (messaging.RunEventRecord, error) {
	payload, err := archivePayload(body)
	if err != nil {
		return messaging.RunEventRecord{}, fmt.Errorf("archive %s payload: %w", body.Event, err)
	}
	return messaging.RunEventRecord{
		Seq:         body.Seq,
		LastSeq:     body.Seq,
		Type:        string(body.Event),
		Timestamp:   occurredAt.UnixMilli(),
		Payload:     payload,
		AssistantID: assistantID,
	}, nil
}

func archivePayload(body messaging.RunEventBody) (json.RawMessage, error) {
	var value any
	switch body.Event {
	case messaging.RunEventRunStarted:
		return nil, nil
	case messaging.RunEventMessageDelta, messaging.RunEventReasoningDelta,
		messaging.RunEventMessageCompleted:
		value = struct {
			MessageID string                  `json:"message_id,omitempty"`
			Role      messaging.MessageRole   `json:"role,omitempty"`
			Content   string                  `json:"content,omitempty"`
			Usage     *messaging.UsagePayload `json:"usage,omitempty"`
		}{
			MessageID: body.Payload.MessageID,
			Role:      body.Payload.Role,
			Content:   body.Payload.Content,
			Usage:     body.Payload.Usage,
		}
	case messaging.RunEventToolCallStarted:
		value = body.Payload.ToolCall
	case messaging.RunEventToolCallFinished:
		value = body.Payload.ToolResult
	case messaging.RunEventTodoSnapshot, messaging.RunEventTodoUpdated:
		value = body.Payload.Todos
	case messaging.RunEventArtifactDeclared:
		value = body.Payload.Artifact
	case messaging.RunEventApprovalRequested:
		value = body.Payload.ApprovalRequest
	case messaging.RunEventApprovalResolved:
		value = body.Payload.ApprovalDecision
	case messaging.RunEventQuestionAsked:
		value = body.Payload.QuestionRequest
	case messaging.RunEventQuestionAnswered:
		value = body.Payload.QuestionAnswer
	case messaging.RunEventPlanPublished:
		value = body.Payload.PlanPublished
	case messaging.RunEventWorkTitleUpdated:
		value = body.Payload.WorkTitle
	default:
		value = body.Payload
	}
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func mergeArchivedDeltas(records []messaging.RunEventRecord) []messaging.RunEventRecord {
	type mergeKey struct {
		eventType string
		messageID string
	}
	merged := make(map[mergeKey]int)
	result := make([]messaging.RunEventRecord, 0, len(records))
	for _, record := range records {
		if record.Type != string(messaging.RunEventMessageDelta) &&
			record.Type != string(messaging.RunEventReasoningDelta) {
			result = append(result, record)
			continue
		}
		var payload struct {
			MessageID string                `json:"message_id"`
			Role      messaging.MessageRole `json:"role,omitempty"`
			Content   string                `json:"content"`
		}
		if json.Unmarshal(record.Payload, &payload) != nil || payload.MessageID == "" {
			result = append(result, record)
			continue
		}
		key := mergeKey{eventType: record.Type, messageID: payload.MessageID}
		if index, ok := merged[key]; ok {
			var existing struct {
				MessageID string                `json:"message_id"`
				Role      messaging.MessageRole `json:"role,omitempty"`
				Content   string                `json:"content"`
			}
			if json.Unmarshal(result[index].Payload, &existing) == nil {
				existing.Content += payload.Content
				if data, err := json.Marshal(existing); err == nil {
					result[index].Payload = data
					result[index].LastSeq = record.Seq
					continue
				}
			}
		}
		merged[key] = len(result)
		result = append(result, record)
	}
	sort.SliceStable(result, func(i, k int) bool { return result[i].Seq < result[k].Seq })
	return result
}

func cloneRunEventBody(body messaging.RunEventBody) (messaging.RunEventBody, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return messaging.RunEventBody{}, err
	}
	var cloned messaging.RunEventBody
	if err := json.Unmarshal(data, &cloned); err != nil {
		return messaging.RunEventBody{}, err
	}
	return cloned, nil
}

func cloneRunEventRecords(records []messaging.RunEventRecord) []messaging.RunEventRecord {
	result := make([]messaging.RunEventRecord, len(records))
	for index, record := range records {
		result[index] = record
		result[index].Payload = append(json.RawMessage(nil), record.Payload...)
	}
	return result
}

func cloneEventContext(eventContext EventContext) EventContext {
	eventContext.ReplyToMessageIDs = append([]string(nil), eventContext.ReplyToMessageIDs...)
	eventContext.MemberCommandIDs = append([]string(nil), eventContext.MemberCommandIDs...)
	return eventContext
}

func usageFromMessaging(usage *messaging.UsagePayload) *agentrundomain.Usage {
	if usage == nil {
		usage = &messaging.UsagePayload{}
	}
	return &agentrundomain.Usage{
		TotalTokens:       usage.InputTokens + usage.OutputTokens,
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CacheInputTokens:  usage.CacheInputTokens,
		CacheOutputTokens: usage.CacheOutputTokens,
	}
}

func isTerminalRunEvent(eventType messaging.RunEventType) bool {
	return eventType == messaging.RunEventRunCompleted ||
		eventType == messaging.RunEventRunFailed ||
		eventType == messaging.RunEventRunCancelled
}

type journalFactory struct{}

// NewJournalFactory creates the default JournalFactory.
func NewJournalFactory() JournalFactory {
	return &journalFactory{}
}

func (*journalFactory) New(
	req *agentrundomain.RunRequest,
	eventContext EventContext,
	publisher RunEventPublisher,
) Journal {
	return NewJournal(req, eventContext, publisher)
}
