package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

type runJournal struct {
	mu      sync.Mutex
	runID   string
	traceID string
	next    agent.EventSink

	maxSeq    int64
	startedAt time.Time
	rawEvents []agent.Event

	toolCalls     int
	toolFailures  int
	toolNames     []string
	usedSkillTool bool
	messageCount  int
	usage         *agent.Usage
	toolRecords   []agent.ToolCallRecord
}

// NewJournal creates a Journal bound to a request and downstream sink.
func NewJournal(req *assistantdomain.RunRequest, sink agent.EventSink) Journal {
	journal := &runJournal{next: noopSink{}}
	if sink != nil {
		journal.next = sink
	}
	if req != nil {
		journal.runID = req.RunID
		journal.traceID = req.TraceID
	}
	return journal
}

func (j *runJournal) Record(ctx context.Context, event *agent.Event) error {
	if j == nil || event == nil {
		return nil
	}
	j.mu.Lock()
	j.normalizeLocked(event)
	if !isTerminalEvent(event.Type) {
		j.rawEvents = append(j.rawEvents, cloneEvent(event))
	}
	j.observeLocked(event)
	j.mu.Unlock()
	return j.next.Emit(ctx, event)
}

func (j *runJournal) Snapshot() JournalSnapshot {
	if j == nil {
		return JournalSnapshot{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var usage *agent.Usage
	if j.usage != nil {
		copied := *j.usage
		usage = &copied
	}
	return JournalSnapshot{
		ToolCalls:    append([]agent.ToolCallRecord(nil), j.toolRecords...),
		Usage:        usage,
		MessageCount: j.messageCount,
		ToolFailures: j.toolFailures,
		ToolNames:    append([]string(nil), j.toolNames...),
		Events:       archiveEvents(j.rawEvents),
	}
}

func (j *runJournal) normalizeLocked(event *agent.Event) {
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
}

func (j *runJournal) observeLocked(event *agent.Event) {
	switch event.Type {
	case "run.started":
		if j.startedAt.IsZero() {
			j.startedAt = event.CreatedAt
		}
	case "tool_call.started":
		j.toolCalls++
		name := toolName(event)
		if name != "" {
			j.toolNames = append(j.toolNames, name)
			j.usedSkillTool = j.usedSkillTool || name == "skill_use"
		}
	case "tool_call.failed":
		j.toolFailures++
		j.recordToolResultLocked(event)
	case "tool_call.completed":
		j.recordToolResultLocked(event)
	case "message.result":
		j.messageCount++
		j.recordUsageLocked(event)
	}
}

func (j *runJournal) recordToolResultLocked(event *agent.Event) {
	var payload struct {
		ToolCallID string          `json:"tool_call_id"`
		Name       string          `json:"name"`
		Result     json.RawMessage `json:"result"`
		Error      string          `json:"error"`
	}
	if len(event.Payload) == 0 || json.Unmarshal(event.Payload, &payload) != nil {
		return
	}
	j.toolRecords = append(j.toolRecords, agent.ToolCallRecord{
		CallID: payload.ToolCallID,
		Name:   payload.Name,
		Result: json.RawMessage(payload.Result),
		Error:  payload.Error,
	})
}

func (j *runJournal) recordUsageLocked(event *agent.Event) {
	var payload struct {
		Usage *agent.Usage `json:"usage"`
	}
	if len(event.Payload) == 0 || json.Unmarshal(event.Payload, &payload) != nil || payload.Usage == nil {
		return
	}
	copied := *payload.Usage
	j.usage = &copied
}

func archiveEvents(events []agent.Event) []JournalEventRecord {
	records := make([]JournalEventRecord, 0, len(events))
	type mergeKey struct {
		eventType agent.EventType
		messageID string
	}
	merged := make(map[mergeKey]int)
	for _, event := range events {
		if event.Type == "run.completed" || event.Type == "run.failed" ||
			event.Type == "run.cancelled" || event.Type == "message.result" {
			continue
		}
		record := JournalEventRecord{
			Seq:       event.Seq,
			LastSeq:   event.Seq,
			Type:      event.Type,
			Timestamp: event.CreatedAt.UnixMilli(),
			Payload:   append(json.RawMessage(nil), event.Payload...),
		}
		if event.Type == "message.delta" || event.Type == "reasoning.delta" {
			var payload struct {
				MessageID string `json:"message_id"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil && payload.MessageID != "" {
				key := mergeKey{eventType: event.Type, messageID: payload.MessageID}
				if index, ok := merged[key]; ok {
					records[index].LastSeq = event.Seq
					continue
				}
				merged[key] = len(records)
			}
		}
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, k int) bool { return records[i].Seq < records[k].Seq })
	return records
}

func toolName(event *agent.Event) string {
	if event == nil {
		return ""
	}
	var payload struct {
		Name string `json:"name"`
	}
	if len(event.Payload) > 0 && json.Unmarshal(event.Payload, &payload) == nil {
		return strings.TrimSpace(payload.Name)
	}
	return ""
}

func cloneEvent(event *agent.Event) agent.Event {
	copied := *event
	copied.Payload = append(json.RawMessage(nil), event.Payload...)
	return copied
}

func isTerminalEvent(eventType agent.EventType) bool {
	return eventType == "run.completed" || eventType == "run.failed" || eventType == "run.cancelled"
}

type noopSink struct{}

func (noopSink) Emit(context.Context, *agent.Event) error { return nil }

type journalFactory struct{}

// NewJournalFactory creates the default assistant JournalFactory.
func NewJournalFactory() JournalFactory {
	return &journalFactory{}
}

func (*journalFactory) New(req *assistantdomain.RunRequest, sink agent.EventSink) Journal {
	return NewJournal(req, sink)
}
