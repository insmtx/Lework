package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
)

// RunTrace is the run-scoped fact snapshot used by learning and final archives.
type RunTrace struct {
	ToolCalls     int
	ToolFailures  int
	ToolNames     []string
	UsedSkillTool bool
	Events        []events.RunEventRecord
}

// RunJournal is the only entrypoint for runtime events during one run.
type RunJournal struct {
	mu            sync.Mutex
	runID         string
	traceID       string
	next          events.Sink
	maxSeq        int64
	startedAt     time.Time
	rawEvents     []events.Event
	messageIDs    *events.MessageIDMapper
	toolCalls     int
	toolFailures  int
	toolNames     []string
	usedSkillTool bool
}

// NewRunJournal creates a run-scoped event journal and downstream broadcaster.
func NewRunJournal(req *agent.RequestContext, next events.Sink) *RunJournal {
	journal := &RunJournal{
		next: events.NewNoopSink(),
	}
	if next != nil {
		journal.next = next
	}
	if req != nil {
		journal.runID = req.RunID
		journal.traceID = req.TraceID
	}
	return journal
}

// Append normalizes, records, and broadcasts one canonical runtime event.
func (j *RunJournal) Append(ctx context.Context, event *events.Event) error {
	if j == nil || event == nil {
		return nil
	}

	j.mu.Lock()
	j.normalizeLocked(event)
	if event.Type != events.EventCompleted {
		j.rawEvents = append(j.rawEvents, cloneEvent(event))
	}
	j.observeStatsLocked(event)
	j.mu.Unlock()

	if j.next == nil {
		return nil
	}
	return j.next.Emit(ctx, event)
}

// Emit implements events.Sink so concrete runtimes can write through the journal.
func (j *RunJournal) Emit(ctx context.Context, event *events.Event) error {
	return j.Append(ctx, event)
}

// Trace returns the learning-facing snapshot from the same source used for archives.
func (j *RunJournal) Trace() *RunTrace {
	if j == nil {
		return &RunTrace{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	return &RunTrace{
		ToolCalls:     j.toolCalls,
		ToolFailures:  j.toolFailures,
		ToolNames:     append([]string{}, j.toolNames...),
		UsedSkillTool: j.usedSkillTool,
		Events:        archiveEventsLocked(j.rawEvents),
	}
}

// CompletedPayload builds the run.completed archive from the recorded event facts.
func (j *RunJournal) CompletedPayload(result *agent.RunResult) events.RunCompletedPayload {
	if result == nil {
		return events.RunCompletedPayload{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	return events.RunCompletedPayload{
		Status: string(result.Status),
		Result: events.RunResultPayload{
			Message: resultMessage(result),
		},
		Usage:       result.Usage,
		Events:      archiveEventsLocked(j.rawEvents),
		StartedAt:   result.StartedAt,
		CompletedAt: result.CompletedAt,
		Metadata:    copyMetadata(result.Metadata),
	}
}

// StartedAt returns the time recorded by the run.started event.
func (j *RunJournal) StartedAt() time.Time {
	if j == nil {
		return time.Time{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.startedAt
}

func resultMessage(result *agent.RunResult) string {
	if result == nil {
		return ""
	}
	if strings.TrimSpace(result.Message) != "" {
		return result.Message
	}
	return result.Error
}

func (j *RunJournal) normalizeLocked(event *events.Event) {
	if event.RunID == "" {
		event.RunID = j.runID
	}
	if event.TraceID == "" {
		event.TraceID = j.traceID
	}
	if event.Seq <= j.maxSeq {
		j.maxSeq++
		event.Seq = j.maxSeq
	} else {
		j.maxSeq = event.Seq
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.ID == "" && event.RunID != "" {
		event.ID = fmt.Sprintf("%s:%d", event.RunID, event.Seq)
	}
	if event.Type == events.EventMessageDelta || event.Type == events.EventReasoningDelta {
		j.ensureMessageIDLocked(event)
	}
}

func (j *RunJournal) ensureMessageIDLocked(event *events.Event) {
	payload, err := events.DecodePayload[events.MessageDeltaPayload](event)
	if err != nil {
		payload = events.MessageDeltaPayload{
			Role:    string(events.MessageRoleAssistant),
			Content: event.Content,
		}
	}
	if strings.TrimSpace(payload.MessageID) == "" {
		if j.messageIDs == nil {
			j.messageIDs = events.NewMessageIDMapper()
		}
		payload.MessageID = j.messageIDs.CurrentOrNew()
	}
	if strings.TrimSpace(payload.Role) == "" {
		payload.Role = string(events.MessageRoleAssistant)
	}
	next := events.NewMessageDelta(payload.MessageID, payload.Content)
	if event.Type == events.EventReasoningDelta {
		next = events.NewReasoningDelta(payload.MessageID, payload.Content)
	}
	event.Payload = next.Payload
	if event.Content == "" {
		event.Content = payload.Content
	}
}

func (j *RunJournal) observeStatsLocked(event *events.Event) {
	switch event.Type {
	case events.EventStarted:
		if j.startedAt.IsZero() {
			j.startedAt = event.CreatedAt
		}
	case events.EventToolCallStarted:
		j.toolCalls++
		if name := toolNameFromEvent(event); name != "" {
			j.toolNames = append(j.toolNames, name)
			if name == "skill_use" {
				j.usedSkillTool = true
			}
		}
	case events.EventToolCallFailed:
		j.toolFailures++
	}
}

func cloneEvent(event *events.Event) events.Event {
	copied := *event
	if len(event.Payload) > 0 {
		copied.Payload = append(events.RawPayload(nil), event.Payload...)
	}
	return copied
}

type mergeKey struct {
	eventType events.EventType
	messageID string
}

func archiveEventsLocked(source []events.Event) []events.RunEventRecord {
	records := make([]events.RunEventRecord, 0, len(source))
	merged := map[mergeKey]int{}
	for _, event := range source {
		if event.Type == events.EventCompleted || event.Type == events.EventResult {
			continue
		}
		if event.Type == events.EventMessageDelta || event.Type == events.EventReasoningDelta {
			record := eventRecord(event)
			payload, err := events.DecodePayload[events.MessageDeltaPayload](&event)
			if err == nil {
				key := mergeKey{eventType: event.Type, messageID: strings.TrimSpace(payload.MessageID)}
				if key.messageID != "" {
					if index, ok := merged[key]; ok {
						records[index].LastSeq = event.Seq
						records[index].Payload = mergedMessagePayload(
							event.Type,
							payload.MessageID,
							messageContentFromPayload(records[index].Payload)+payload.Content,
						)
						continue
					}
					record.Payload = mergedMessagePayload(event.Type, payload.MessageID, payload.Content)
					merged[key] = len(records)
				}
			}
			records = append(records, record)
			continue
		}
		records = append(records, eventRecord(event))
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Seq < records[j].Seq
	})
	return records
}

func eventRecord(event events.Event) events.RunEventRecord {
	return events.RunEventRecord{
		Seq:       event.Seq,
		LastSeq:   event.Seq,
		Type:      event.Type,
		Timestamp: event.CreatedAt.UnixMilli(),
		Payload:   archivedPayload(event),
	}
}

func archivedPayload(event events.Event) events.RawPayload {
	if len(event.Payload) > 0 {
		return append(events.RawPayload(nil), event.Payload...)
	}
	if event.Type != events.EventResult || strings.TrimSpace(event.Content) == "" {
		return nil
	}
	raw, err := json.Marshal(events.RunResultPayload{Message: event.Content})
	if err != nil {
		return nil
	}
	return events.RawPayload(raw)
}

func messageContentFromPayload(raw events.RawPayload) string {
	var payload events.MessageDeltaPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return payload.Content
}

func mergedMessagePayload(eventType events.EventType, messageID string, content string) events.RawPayload {
	event := events.NewMessageDelta(messageID, content)
	if eventType == events.EventReasoningDelta {
		event = events.NewReasoningDelta(messageID, content)
	}
	return event.Payload
}

func copyMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}

func toolNameFromEventContent(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Name)
}

func toolNameFromEvent(event *events.Event) string {
	if event == nil {
		return ""
	}
	payload, err := events.DecodePayload[events.ToolCallPayload](event)
	if err == nil && strings.TrimSpace(payload.Name) != "" {
		return strings.TrimSpace(payload.Name)
	}
	resultPayload, err := events.DecodePayload[events.ToolCallResultPayload](event)
	if err == nil && strings.TrimSpace(resultPayload.Name) != "" {
		return strings.TrimSpace(resultPayload.Name)
	}
	return toolNameFromEventContent(event.Content)
}

var _ events.Sink = (*RunJournal)(nil)
