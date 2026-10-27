package acp

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	acpsdk "github.com/coder/acp-go-sdk"
	"github.com/insmtx/Leros/backend/agent"
)

func TestTranslatorMessageChunk(t *testing.T) {
	stats := &Stats{}
	translator := NewTranslator(stats)
	messageID := "m1"

	events := translator.Handle(note(acpsdk.SessionUpdate{
		AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
			Content:   acpsdk.TextBlock("hello"),
			MessageId: &messageID,
		},
	}))
	if len(events) != 1 || events[0].Type != agent.NodeEventMessageUpdate {
		t.Fatalf("events = %+v, want one message.update", events)
	}
	payload, ok := events[0].Payload.(*agent.MessageUpdatePayload)
	if !ok || payload.MessageID != "m1" || payload.Content != "hello" || payload.Role != "assistant" {
		t.Fatalf("payload = %+v", events[0].Payload)
	}
	if got := translator.FinalText(); got != "hello" {
		t.Fatalf("final text = %q, want hello", got)
	}
	if got := stats.Snapshot().DroppedEvents; got != 0 {
		t.Fatalf("dropped = %d, want 0", got)
	}
}

func TestTranslatorMessageChunkFallbackMessageID(t *testing.T) {
	translator := NewTranslator(nil)
	first := "m1"
	translator.Handle(note(acpsdk.SessionUpdate{
		AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
			Content:   acpsdk.TextBlock("a"),
			MessageId: &first,
		},
	}))

	events := translator.Handle(note(acpsdk.SessionUpdate{
		AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
			Content: acpsdk.TextBlock("b"),
		},
	}))
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	payload := events[0].Payload.(*agent.MessageUpdatePayload)
	if payload.MessageID != "m1" {
		t.Fatalf("messageID = %q, want fallback m1", payload.MessageID)
	}
	if got := translator.FinalText(); got != "ab" {
		t.Fatalf("final text = %q, want ab", got)
	}
}

func TestTranslatorDropsEmptyMessageID(t *testing.T) {
	stats := &Stats{}
	translator := NewTranslator(stats)

	events := translator.Handle(note(acpsdk.SessionUpdate{
		AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
			Content: acpsdk.TextBlock("orphan"),
		},
	}))
	if len(events) != 0 {
		t.Fatalf("events = %d, want 0 (empty messageID must be dropped)", len(events))
	}
	if got := stats.Snapshot().DroppedEvents; got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
}

func TestTranslatorReasoningStripsSuffixAndDedupes(t *testing.T) {
	stats := &Stats{}
	translator := NewTranslator(stats)

	reasoningID := "m1:reasoning:1"
	first := translator.Handle(note(acpsdk.SessionUpdate{
		AgentThoughtChunk: &acpsdk.SessionUpdateAgentThoughtChunk{
			Content:   acpsdk.TextBlock("plan"),
			MessageId: &reasoningID,
		},
	}))
	if len(first) != 1 {
		t.Fatalf("events = %d, want 1", len(first))
	}
	payload := first[0].Payload.(*agent.ReasoningUpdatePayload)
	if payload.MessageID != "m1" || !strings.HasPrefix(payload.Content, "plan") {
		t.Fatalf("payload = %+v, want stripped message id m1", payload)
	}

	duplicate := translator.Handle(note(acpsdk.SessionUpdate{
		AgentThoughtChunk: &acpsdk.SessionUpdateAgentThoughtChunk{
			Content:   acpsdk.TextBlock("plan"),
			MessageId: &reasoningID,
		},
	}))
	if len(duplicate) != 0 {
		t.Fatalf("duplicate reasoning produced %d events, want 0", len(duplicate))
	}
	if got := stats.Snapshot().DroppedEvents; got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}

	cumulative := translator.Handle(note(acpsdk.SessionUpdate{
		AgentThoughtChunk: &acpsdk.SessionUpdateAgentThoughtChunk{
			Content:   acpsdk.TextBlock("plan more"),
			MessageId: &reasoningID,
		},
	}))
	if len(cumulative) != 1 {
		t.Fatalf("events = %d, want 1", len(cumulative))
	}
	delta := cumulative[0].Payload.(*agent.ReasoningUpdatePayload)
	if delta.Content != " more" {
		t.Fatalf("delta = %q, want only the appended part", delta.Content)
	}
}

func TestTranslatorToolCallLifecycle(t *testing.T) {
	translator := NewTranslator(nil)
	state := toolCallUpdate(
		acpsdk.ToolCallStatusPending,
		acpsdk.ToolKindExecute, "echo hi", nil,
	)
	if events := translator.Handle(note(state)); len(events) != 0 {
		t.Fatalf("pending tool_call should not emit, got %d", len(events))
	}

	inProgress := toolCallUpdate(acpsdk.ToolCallStatusInProgress, acpsdk.ToolKindExecute, "", nil)
	events := translator.Handle(note(inProgress))
	if len(events) != 1 || events[0].Type != agent.NodeEventToolExecutionStart {
		t.Fatalf("events = %+v, want one tool_execution.start", events)
	}
	start := events[0].Payload.(*agent.ToolExecutionStartPayload)
	if start.Name != "bash" || start.ToolCallID == "" {
		t.Fatalf("start = %+v, want display name bash", start)
	}
	if string(start.Arguments) != `{"command":"echo hi"}` {
		t.Fatalf("arguments = %s", start.Arguments)
	}

	if events := translator.Handle(note(inProgress)); len(events) != 0 {
		t.Fatalf("second in_progress should be heartbeat only, got %d", len(events))
	}

	completed := toolCallUpdate(acpsdk.ToolCallStatusCompleted, "", "", textBlocks("done"))
	events = translator.Handle(note(completed))
	if len(events) != 1 || events[0].Type != agent.NodeEventToolExecutionEnd {
		t.Fatalf("events = %+v, want tool_execution.end", events)
	}
	end := events[0].Payload.(*agent.ToolExecutionEndPayload)
	if end.Name != "bash" || end.IsError || string(end.Result) != `"done"` {
		t.Fatalf("end = %+v", end)
	}
}

func TestTranslatorCompletedWithoutStartEmitsStart(t *testing.T) {
	translator := NewTranslator(nil)
	translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusPending, acpsdk.ToolKindRead, "read", nil)))

	events := translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusCompleted, "", "", textBlocks("file"))))
	if len(events) != 2 {
		t.Fatalf("events = %d, want start+end", len(events))
	}
	if events[0].Type != agent.NodeEventToolExecutionStart || events[1].Type != agent.NodeEventToolExecutionEnd {
		t.Fatalf("event types = %s, %s", events[0].Type, events[1].Type)
	}
	if name := events[1].Payload.(*agent.ToolExecutionEndPayload).Name; name != "read" {
		t.Fatalf("end name = %q, want read", name)
	}
}

func TestTranslatorFailedTool(t *testing.T) {
	translator := NewTranslator(nil)
	translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusPending, acpsdk.ToolKindExecute, "false", nil)))
	translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusInProgress, "", "", nil)))

	events := translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusFailed, "", "", textBlocks("boom"))))
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	end := events[0].Payload.(*agent.ToolExecutionEndPayload)
	if !end.IsError || end.Error != "boom" || end.Name != "bash" {
		t.Fatalf("end = %+v", end)
	}
}

func TestTranslatorToolNameMapping(t *testing.T) {
	tests := []struct {
		name  string
		kind  acpsdk.ToolKind
		title string
		want  string
	}{
		{"shell via kind", acpsdk.ToolKindExecute, "ls -la", "bash"},
		{"read via title", acpsdk.ToolKindRead, "read", "read"},
		{"patch display", acpsdk.ToolKindEdit, "patch", "apply_patch"},
		{"subagent display", acpsdk.ToolKindOther, "subagent", "task"},
		{"child title prefix", acpsdk.ToolKindRead, "child task: read", "read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator(nil)
			translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusPending, tt.kind, tt.title, nil)))
			events := translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusInProgress, "", "", nil)))
			if len(events) != 1 {
				t.Fatalf("events = %d", len(events))
			}
			start := events[0].Payload.(*agent.ToolExecutionStartPayload)
			if start.Name != tt.want {
				t.Fatalf("name = %q, want %q", start.Name, tt.want)
			}
		})
	}
}

func TestTranslatorChildSession(t *testing.T) {
	stats := &Stats{}
	translator := NewTranslator(stats)
	messageID := "child-msg"

	events := translator.Handle(acpsdk.SessionNotification{
		Meta: map[string]any{childSessionMetaKey: map[string]any{"id": "child"}},
		Update: acpsdk.SessionUpdate{
			AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
				Content:   acpsdk.TextBlock("child monologue"),
				MessageId: &messageID,
			},
		},
	})
	if len(events) != 0 {
		t.Fatalf("child text should be dropped, got %d events", len(events))
	}

	childMeta := map[string]any{childSessionMetaKey: map[string]any{"id": "child"}}
	translator.Handle(acpsdk.SessionNotification{
		Meta: childMeta,
		Update: acpsdk.SessionUpdate{ToolCall: &acpsdk.SessionUpdateToolCall{
			ToolCallId: "c-child", Kind: acpsdk.ToolKindRead, Title: "child task: read",
			Status: acpsdk.ToolCallStatusPending,
		}},
	})
	events = translator.Handle(acpsdk.SessionNotification{
		Meta: childMeta,
		Update: acpsdk.SessionUpdate{ToolCallUpdate: &acpsdk.SessionToolCallUpdate{
			ToolCallId: "c-child",
			Status:     statusPtr(acpsdk.ToolCallStatusInProgress),
		}},
	})
	if len(events) != 1 {
		t.Fatalf("child tool should be kept, got %d events", len(events))
	}
	if name := events[0].Payload.(*agent.ToolExecutionStartPayload).Name; name != "read" {
		t.Fatalf("name = %q, want read (prefix stripped)", name)
	}
}

func TestTranslatorFiltersTools(t *testing.T) {
	for _, tool := range []string{"todowrite", "question", "artifact_declare"} {
		t.Run(tool, func(t *testing.T) {
			translator := NewTranslator(nil)
			translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusPending, acpsdk.ToolKindOther, tool, nil)))
			if events := translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusInProgress, "", "", nil))); len(events) != 0 {
				t.Fatalf("filtered tool emitted start: %+v", events)
			}
			if events := translator.Handle(note(toolCallUpdate(acpsdk.ToolCallStatusCompleted, "", "", textBlocks("x")))); len(events) != 0 {
				t.Fatalf("filtered tool emitted end: %+v", events)
			}
		})
	}
}

func TestTranslatorUnknownUpdateCounts(t *testing.T) {
	stats := &Stats{}
	translator := NewTranslator(stats)

	if events := translator.Handle(note(acpsdk.SessionUpdate{})); len(events) != 0 {
		t.Fatalf("unknown update emitted events: %+v", events)
	}
	if got := stats.Snapshot().UnknownUpdates; got != 1 {
		t.Fatalf("unknown count = %d, want 1", got)
	}
}

func TestTranslatorKnownNoops(t *testing.T) {
	stats := &Stats{}
	translator := NewTranslator(stats)
	updates := []acpsdk.SessionUpdate{
		{UserMessageChunk: &acpsdk.SessionUpdateUserMessageChunk{Content: acpsdk.TextBlock("u")}},
		{Plan: &acpsdk.SessionUpdatePlan{}},
		{PlanUpdate: &acpsdk.SessionPlanUpdate{}},
		{PlanRemoved: &acpsdk.SessionUpdatePlanRemoved{}},
		{AvailableCommandsUpdate: &acpsdk.SessionAvailableCommandsUpdate{}},
		{CurrentModeUpdate: &acpsdk.SessionCurrentModeUpdate{}},
		{ConfigOptionUpdate: &acpsdk.SessionConfigOptionUpdate{}},
		{SessionInfoUpdate: &acpsdk.SessionSessionInfoUpdate{}},
		{UsageUpdate: &acpsdk.SessionUsageUpdate{Size: 10, Used: 1}},
	}
	for _, update := range updates {
		if events := translator.Handle(note(update)); len(events) != 0 {
			t.Fatalf("noop update emitted %d events", len(events))
		}
	}
	if got := stats.Snapshot().UnknownUpdates; got != 0 {
		t.Fatalf("known noops counted as unknown: %d", got)
	}
}

func TestTruncateToolOutput(t *testing.T) {
	small := "hello"
	if got := TruncateToolOutput(small); got != small {
		t.Fatalf("small output changed: %q", got)
	}

	large := strings.Repeat("a", MaxToolResultBytes+100)
	got := TruncateToolOutput(large)
	if len(got) <= MaxToolResultBytes {
		t.Fatalf("truncated length = %d", len(got))
	}
	if !strings.Contains(got, "...[truncated 100 bytes]") {
		t.Fatalf("missing truncation marker: %q", got[len(got)-40:])
	}

	multibyte := strings.Repeat("中", MaxToolResultBytes/2)
	got = TruncateToolOutput(multibyte)
	if !utf8.ValidString(got) {
		t.Fatal("truncated multibyte output is not valid UTF-8")
	}
}

func note(update acpsdk.SessionUpdate) acpsdk.SessionNotification {
	return acpsdk.SessionNotification{SessionId: "s1", Update: update}
}

func toolCallUpdate(
	status acpsdk.ToolCallStatus,
	kind acpsdk.ToolKind,
	title string,
	content []acpsdk.ToolCallContent,
) acpsdk.SessionUpdate {
	update := &acpsdk.SessionToolCallUpdate{
		ToolCallId: "c1",
		Status:     statusPtr(status),
		Content:    content,
	}
	if kind != "" {
		update.Kind = kindPtr(kind)
	}
	if title != "" {
		update.Title = &title
	}
	if status == acpsdk.ToolCallStatusPending {
		return acpsdk.SessionUpdate{ToolCall: &acpsdk.SessionUpdateToolCall{
			ToolCallId: "c1",
			Kind:       kind,
			Title:      title,
			Status:     status,
			RawInput:   map[string]any{"command": "echo hi"},
		}}
	}
	return acpsdk.SessionUpdate{ToolCallUpdate: update}
}

func textBlocks(text string) []acpsdk.ToolCallContent {
	return []acpsdk.ToolCallContent{{
		Content: &acpsdk.ToolCallContentContent{Content: acpsdk.TextBlock(text), Type: "content"},
	}}
}

func statusPtr(status acpsdk.ToolCallStatus) *acpsdk.ToolCallStatus { return &status }

func kindPtr(kind acpsdk.ToolKind) *acpsdk.ToolKind { return &kind }

var _ = json.Marshal
