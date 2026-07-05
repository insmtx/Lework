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
		eventContext: cloneEventContext(eventContext),
		publisher:    publisher,
	}
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

	j.mu.Lock()
	j.maxSeq++
	body.Seq = j.maxSeq
	if len(body.ReplyToMessageIDs) == 0 {
		body.ReplyToMessageIDs = append([]string(nil), j.eventContext.ReplyToMessageIDs...)
	}
	j.observeLocked(body)
	if !isTerminalRunEvent(body.Event) {
		record, recordErr := archiveRecord(body, occurredAt)
		if recordErr != nil {
			j.mu.Unlock()
			return recordErr
		}
		j.events = append(j.events, record)
	}
	event := j.envelopeLocked(body, occurredAt)
	publisher := j.publisher
	if publisher == nil {
		j.mu.Unlock()
		return nil
	}
	err = publisher.PublishRunEvent(ctx, event)
	j.mu.Unlock()
	return err
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
			OrgID:     j.eventContext.OrgID,
			WorkerID:  j.eventContext.WorkerID,
			SessionID: j.eventContext.SessionID,
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
) (messaging.RunEventRecord, error) {
	payload, err := archivePayload(body)
	if err != nil {
		return messaging.RunEventRecord{}, fmt.Errorf("archive %s payload: %w", body.Event, err)
	}
	return messaging.RunEventRecord{
		Seq:       body.Seq,
		LastSeq:   body.Seq,
		Type:      string(body.Event),
		Timestamp: occurredAt.UnixMilli(),
		Payload:   payload,
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
