package modelrouter

import (
	"testing"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Text stream: delta arrives without ContentStart
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_TextDeltaBeforeContentStart(t *testing.T) {
	ag := NewStreamAggregator()

	// Simulate: Chat adapter emits IRStreamContentDelta without preceding Start.
	// This is the exact scenario that caused "OutputTextDelta without active item".
	events := ag.ProcessIREvent(&IRStreamEvent{
		Type:      IRStreamContentDelta,
		DeltaText: "Hello",
		Index:     0,
	})

	// Should produce: response.created + output_item.added (text) + original delta
	if len(events) != 3 {
		t.Fatalf("expected 3 events (message_start + content_start + delta), got %d", len(events))
	}
	if events[0].Type != IRStreamMessageStart {
		t.Errorf("events[0].Type = %q, want message_start", events[0].Type)
	}
	if events[1].Type != IRStreamContentStart {
		t.Errorf("events[1].Type = %q, want content_part_start", events[1].Type)
	}
	if events[1].Part == nil || events[1].Part.Type != IRPartText {
		t.Errorf("events[1].Part should be text, got %v", events[1].Part)
	}
	if events[2].Type != IRStreamContentDelta || events[2].DeltaText != "Hello" {
		t.Errorf("events[2] = %v, want content_part_delta with 'Hello'", events[2])
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Normal text stream: Start → Delta → Stop → Done
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_NormalTextStream(t *testing.T) {
	ag := NewStreamAggregator()

	feed := func(evt *IRStreamEvent) []*IRStreamEvent {
		return ag.ProcessIREvent(evt)
	}

	// Message start (response.created)
	evt := feed(&IRStreamEvent{Type: IRStreamMessageStart, ResponseID: "resp_1", ResponseModel: "gpt-5"})
	if len(evt) != 1 || evt[0].Type != IRStreamMessageStart {
		t.Fatalf("message_start: %v", evt)
	}

	// Content start (output_item.added for text)
	evt = feed(&IRStreamEvent{Type: IRStreamContentStart, Index: 0, Part: &IRContentPart{Type: IRPartText}})
	if len(evt) != 1 || evt[0].Type != IRStreamContentStart {
		t.Fatalf("content_start: %v", evt)
	}

	// Text delta
	evt = feed(&IRStreamEvent{Type: IRStreamContentDelta, Index: 0, DeltaText: "Hi"})
	if len(evt) != 1 || evt[0].DeltaText != "Hi" {
		t.Fatalf("text delta: %v", evt)
	}

	// Another text delta — no extra preamble needed
	evt = feed(&IRStreamEvent{Type: IRStreamContentDelta, Index: 0, DeltaText: " there"})
	if len(evt) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evt))
	}

	// Content stop
	evt = feed(&IRStreamEvent{Type: IRStreamContentStop, Index: 0})
	if len(evt) != 1 || evt[0].Type != IRStreamContentStop {
		t.Fatalf("content_stop: %v", evt)
	}

	// Message delta + done
	feed(&IRStreamEvent{Type: IRStreamMessageDelta, StopReason: IRStopEndTurn})
	evt = feed(&IRStreamEvent{Type: IRStreamDone, Usage: &IRUsage{InputTokens: 10, OutputTokens: 5}})

	// Should produce: message_delta + done (2 events, no dangling cleanup needed)
	if len(evt) != 2 {
		t.Fatalf("expected 2 events (message_delta + done), got %d: %v", len(evt), eventTypes(evt))
	}
	if evt[0].Type != IRStreamMessageDelta {
		t.Errorf("evt[0].Type = %q", evt[0].Type)
	}
	if evt[1].Type != IRStreamDone {
		t.Errorf("evt[1].Type = %q", evt[1].Type)
	}

	// Double done — should be ignored
	evt = feed(&IRStreamEvent{Type: IRStreamDone})
	if len(evt) != 0 {
		t.Errorf("double done should return nil, got %d events", len(evt))
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Reasoning content (DeepSeek thinking mode)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_ReasoningDelta(t *testing.T) {
	ag := NewStreamAggregator()

	// Reasoning delta arrives first — no preceding Start, DeltaType = "reasoning"
	events := ag.ProcessIREvent(&IRStreamEvent{
		Type:      IRStreamContentDelta,
		DeltaText: "thinking...",
		DeltaType: "reasoning",
		Index:     0,
	})

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(events), eventTypes(events))
	}
	if events[0].Type != IRStreamMessageStart {
		t.Errorf("events[0].Type = %q, want message_start", events[0].Type)
	}
	// Should be synthesized as ItemReasoning
	if events[1].Type != IRStreamContentStart || events[1].Part == nil || events[1].Part.Type != IRPartReasoning {
		t.Errorf("events[1] should be reasoning content_start, got %v", events[1])
	}
	if events[2].Type != IRStreamContentDelta || events[2].DeltaText != "thinking..." {
		t.Errorf("events[2] = %v", events[2])
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Reasoning followed by text (DeepSeek: thinking then answer)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_ReasoningThenText(t *testing.T) {
	ag := NewStreamAggregator()

	// Reasoning delta (index 0)
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "think", DeltaType: "reasoning", Index: 0})

	// Now normal text delta at index 1 (next output_index)
	events := ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "answer", Index: 1})

	// Should auto-start text item at index 1 (with output_item.added)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (content_start + delta for index 1), got %d: %v", len(events), eventTypes(events))
	}
	if events[0].Type != IRStreamContentStart || events[0].Index != 1 {
		t.Errorf("events[0] should be content_start for index 1, got %v", events[0])
	}
	if events[0].Part.Type != IRPartText {
		t.Errorf("events[0].Part.Type should be text, got %v", events[0].Part.Type)
	}
	if events[1].DeltaText != "answer" {
		t.Errorf("events[1].DeltaText = %q", events[1].DeltaText)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Tool call stream
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_ToolCallStream(t *testing.T) {
	ag := NewStreamAggregator()

	// Tool call delta (index 1)
	events := ag.ProcessIREvent(&IRStreamEvent{
		Type:      IRStreamContentDelta,
		DeltaJSON: `{"q": "test"}`,
		Index:     1,
	})

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(events), eventTypes(events))
	}
	if events[0].Type != IRStreamMessageStart {
		t.Errorf("events[0].Type = %q", events[0].Type)
	}
	if events[1].Type != IRStreamContentStart || events[1].Part == nil || events[1].Part.Type != IRPartToolCall {
		t.Errorf("events[1] should be tool content_start, got %v", events[1])
	}
	if events[2].Type != IRStreamContentDelta || events[2].DeltaJSON != `{"q": "test"}` {
		t.Errorf("events[2] = %v", events[2])
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Dangling cleanup: text block never received Stop before Done
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_DanglingTextCleanup(t *testing.T) {
	ag := NewStreamAggregator()

	// Text delta arrives (but no ContentStart or ContentStop)
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "incomplete", Index: 0})

	// Done arrives — text block should be auto-closed
	events := ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamDone, Usage: &IRUsage{InputTokens: 5, OutputTokens: 3}})

	// expect: message_start (synthesized) + content_start (synthesized for item) + content_part_stop + message_delta + done
	// Wait — content_start was already synthesized when the delta arrived.
	// On Done, the aggregator should emit ContentStop for the dangling item,
	// then MessageDelta, then Done.
	// Count: should NOT include message_start (already sent) and content_start (already sent).
	// Events should be: content_stop + message_delta + done = 3 events.

	if len(events) != 3 {
		t.Fatalf("expected 3 events (content_stop + message_delta + done), got %d: %v", len(events), eventTypes(events))
	}
	if events[0].Type != IRStreamContentStop || events[0].Index != 0 {
		t.Errorf("events[0] should be content_stop index 0, got %v", events[0])
	}
	if events[1].Type != IRStreamMessageDelta {
		t.Errorf("events[1].Type = %q, want message_delta", events[1].Type)
	}
	if events[2].Type != IRStreamDone {
		t.Errorf("events[2].Type = %q, want done", events[2].Type)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Empty response: Done arrives with no prior events at all
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_EmptyResponse(t *testing.T) {
	ag := NewStreamAggregator()

	events := ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamDone, Usage: &IRUsage{InputTokens: 5, OutputTokens: 0}})

	// Should emit: message_start + message_delta + done (no items to clean up)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(events), eventTypes(events))
	}
	if events[0].Type != IRStreamMessageStart {
		t.Errorf("events[0].Type = %q", events[0].Type)
	}
	if events[1].Type != IRStreamMessageDelta {
		t.Errorf("events[1].Type = %q", events[1].Type)
	}
	if events[2].Type != IRStreamDone {
		t.Errorf("events[2].Type = %q", events[2].Type)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Error event: passes through unchanged
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_ErrorPassthrough(t *testing.T) {
	ag := NewStreamAggregator()

	events := ag.ProcessIREvent(&IRStreamEvent{
		Type:         IRStreamError,
		ErrorMessage: "something went wrong",
	})

	if len(events) != 1 || events[0].Type != IRStreamError {
		t.Fatalf("expected 1 error event, got %v", events)
	}
	if events[0].ErrorMessage != "something went wrong" {
		t.Errorf("ErrorMessage = %q", events[0].ErrorMessage)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Multiple output_index isolation (parallel items)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_ParallelItems(t *testing.T) {
	ag := NewStreamAggregator()

	// Item 0: text
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "text 0", Index: 0})
	// Item 1: tool call
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaJSON: "{}", Index: 1})
	// More text on item 0
	events := ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: " more", Index: 0})

	// item 0 already started, so only delta should come through
	if len(events) != 1 || events[0].Type != IRStreamContentDelta {
		t.Fatalf("expected 1 delta for index 0, got %d: %v", len(events), eventTypes(events))
	}
	if events[0].DeltaText != " more" {
		t.Errorf("DeltaText = %q", events[0].DeltaText)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Out-of-order deltas: delta at index N arrives before delta at index 0
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_OutOfOrderDeltas(t *testing.T) {
	ag := NewStreamAggregator()

	// Delta at index 2 arrives first (e.g. tool call before text)
	evt1 := ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaJSON: "{}", Index: 2})
	// Delta at index 0 arrives later
	evt2 := ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "hello", Index: 0})

	// Index 2 should get its own item
	if len(evt1) != 3 {
		t.Fatalf("expected 3 events for index 2, got %d", len(evt1))
	}
	if evt1[1].Type != IRStreamContentStart || evt1[1].Index != 2 {
		t.Errorf("index 2 start: %v", evt1[1])
	}
	// Index 0 should get its own item (separate) — needs output_item.added + delta
	if len(evt2) != 2 {
		t.Fatalf("expected 2 events for index 0 (content_start + delta), got %d: %v", len(evt2), eventTypes(evt2))
	}
	if evt2[0].Type != IRStreamContentStart || evt2[0].Index != 0 {
		t.Errorf("index 0 start: %v", evt2[0])
	}
	if evt2[1].Type != IRStreamContentDelta || evt2[1].DeltaText != "hello" {
		t.Errorf("index 0 delta: %v", evt2[1])
	}

	// Verify both items exist independently
	if len(ag.items) != 2 {
		t.Errorf("expected 2 items, got %d", len(ag.items))
	}
	if ag.items[0] == nil || ag.items[2] == nil {
		t.Errorf("both items should exist: %v", ag.items)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Nil event safety
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_NilEvent(t *testing.T) {
	ag := NewStreamAggregator()
	events := ag.ProcessIREvent(nil)
	if len(events) != 0 {
		t.Errorf("nil event should return nil, got %d events", len(events))
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Helper: extract event types for readable error messages
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func eventTypes(events []*IRStreamEvent) []IRStreamEventType {
	types := make([]IRStreamEventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return types
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Finalize — stream completion lifecycle
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_FinalizeClosesOpenItems(t *testing.T) {
	ag := NewStreamAggregator()

	// Text delta without preceding Start or Stop.
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "data", Index: 0})

	events := ag.Finalize()
	// Expect: content_stop (dangling cleanup) + message_delta + done
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events from Finalize, got %d: %v", len(events), eventTypes(events))
	}

	last := events[len(events)-1]
	if last.Type != IRStreamDone {
		t.Errorf("last event should be done, got %v", last.Type)
	}

	// Verify item was properly stopped.
	stopped := false
	for _, e := range events {
		if e.Type == IRStreamContentStop && e.Index == 0 {
			stopped = true
		}
	}
	if !stopped {
		t.Error("expected content_stop for dangling item at index 0")
	}
}

func TestAggregator_FinalizeIdempotent(t *testing.T) {
	ag := NewStreamAggregator()
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "x", Index: 0})

	first := ag.Finalize()
	if len(first) == 0 {
		t.Fatal("first Finalize should return events")
	}

	second := ag.Finalize()
	if len(second) != 0 {
		t.Fatalf("second Finalize should be no-op, got %d events", len(second))
	}

	third := ag.Finalize()
	if len(third) != 0 {
		t.Fatalf("third Finalize should be no-op, got %d events", len(third))
	}
}

func TestAggregator_IsDoneReportsCorrectly(t *testing.T) {
	ag := NewStreamAggregator()

	if ag.IsDone() {
		t.Error("IsDone should be false before Finalize")
	}

	ag.Finalize()

	if !ag.IsDone() {
		t.Error("IsDone should be true after Finalize")
	}
}

// Simulate EOF without [DONE]: the aggregator was processing events normally,
// then the stream drops.  Finalize must still emit the completed events.
func TestAggregator_EOFMidStream(t *testing.T) {
	ag := NewStreamAggregator()

	// Normal text stream partially through — item started but never stopped.
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamMessageStart, ResponseID: "r", ResponseModel: "m"})
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "partial", Index: 0})

	// EOF — no [DONE], no ContentStop.
	events := ag.Finalize()

	if len(events) == 0 {
		t.Fatal("Finalize after EOF mid-stream should emit events")
	}

	hasDone := false
	for _, e := range events {
		if e.Type == IRStreamDone {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("Finalize after EOF should emit IRStreamDone")
	}
}

func TestAggregator_FinalizeEmptyResponse(t *testing.T) {
	ag := NewStreamAggregator()

	// No events at all — Finalize should still emit a valid completion.
	events := ag.Finalize()

	hasMessageStart := false
	hasDone := false
	for _, e := range events {
		if e.Type == IRStreamMessageStart {
			hasMessageStart = true
		}
		if e.Type == IRStreamDone {
			hasDone = true
		}
	}
	if !hasMessageStart {
		t.Error("Finalize on empty stream should synthesize response.created")
	}
	if !hasDone {
		t.Error("Finalize on empty stream should emit IRStreamDone")
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// P1: Critical path tests — index collision, multi-segment tool args,
//      ContentDelta without Start, DONE→Finalize→completed, EOF→Finalize
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestAggregator_TextAndToolSameIndex(t *testing.T) {
	ag := NewStreamAggregator()

	// Text delta at index 0
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "hello", Index: 0})

	// Tool call delta at index 1 (already remapped by ChatAdapter from OpenAI tool_call.index=0)
	// Even if OpenAI returns text and tool_call both with local index 0,
	// the ChatAdapter remaps tool to IR global index 1.
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaJSON: "{}", Index: 1})

	// Verify both items exist independently with correct types
	if len(ag.items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(ag.items))
	}
	if ag.items[0] == nil || ag.items[0].itemType != ItemText {
		t.Errorf("item[0] should be ItemText, got %v", ag.items[0])
	}
	if ag.items[1] == nil || ag.items[1].itemType != ItemToolCall {
		t.Errorf("item[1] should be ItemToolCall, got %v", ag.items[1])
	}
}

func TestAggregator_MultiSegmentToolArgs(t *testing.T) {
	ag := NewStreamAggregator()

	// Segment 1: tool content_start with id/name
	ag.ProcessIREvent(&IRStreamEvent{
		Type:  IRStreamContentStart,
		Index: 1,
		Part: &IRContentPart{
			Type: IRPartToolCall,
			ToolCall: &IRToolCallPart{
				ID:   "call_001",
				Name: "get_weather",
			},
		},
	})

	// Segment 2: tool args delta via DeltaJSON
	ag.ProcessIREvent(&IRStreamEvent{
		Type:      IRStreamContentDelta,
		Index:     1,
		DeltaJSON: `{"city":"Tokyo"}`,
	})

	// Segment 3: more args (should NOT emit another ContentStart)
	ag.ProcessIREvent(&IRStreamEvent{
		Type:      IRStreamContentDelta,
		Index:     1,
		DeltaJSON: `"`,
	})

	// Item 1 should still exist and be tool type
	if ag.items[1] == nil || ag.items[1].itemType != ItemToolCall {
		t.Fatalf("item[1] should be ItemToolCall, got %v", ag.items[1])
	}
}

func TestAggregator_ContentDeltaWithoutStartAutoComplete(t *testing.T) {
	ag := NewStreamAggregator()

	// Delta arrives with no preceding ContentStart — Aggregator must synthesize it.
	events := ag.ProcessIREvent(&IRStreamEvent{
		Type:      IRStreamContentDelta,
		DeltaText: "data",
		Index:     0,
	})

	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (message_start or content_start + delta), got %d", len(events))
	}

	hasStart := false
	for _, e := range events {
		if e.Type == IRStreamContentStart && e.Index == 0 {
			hasStart = true
		}
	}
	if !hasStart {
		t.Error("Aggregator should have synthesized IRStreamContentStart for index 0")
	}

	hasDelta := false
	for _, e := range events {
		if e.Type == IRStreamContentDelta && e.DeltaText == "data" {
			hasDelta = true
		}
	}
	if !hasDelta {
		t.Error("Aggregator should have forwarded the original delta")
	}
}

func TestAggregator_DONETriggersFinalizeWithCompleted(t *testing.T) {
	ag := NewStreamAggregator()

	// Simulate full stream lifecycle via ProcessIREvent:
	// message_start → text delta → content_stop → message_delta → [DONE] via Finalize()
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamMessageStart, ResponseID: "r", ResponseModel: "m"})
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "done", Index: 0})
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentStop, Index: 0})
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamMessageDelta, StopReason: IRStopEndTurn})

	events := ag.Finalize()

	if len(events) < 1 {
		t.Fatal("Finalize should emit events")
	}

	last := events[len(events)-1]
	if last.Type != IRStreamDone {
		t.Errorf("last event should be IRStreamDone, got %v", last.Type)
	}
	if !ag.IsDone() {
		t.Error("IsDone should be true after Finalize")
	}
}

func TestAggregator_EOFWithoutDoneStillCompletes(t *testing.T) {
	ag := NewStreamAggregator()

	// Stream drops mid-way — no ContentStop or MessageDelta.
	ag.ProcessIREvent(&IRStreamEvent{Type: IRStreamContentDelta, DeltaText: "mid", Index: 0})

	// EOF — Finalize must produce a valid completion.
	events := ag.Finalize()

	if len(events) == 0 {
		t.Fatal("Finalize after EOF mid-stream should emit events")
	}

	hasStop := false
	hasDone := false
	for _, e := range events {
		if e.Type == IRStreamContentStop {
			hasStop = true
		}
		if e.Type == IRStreamDone {
			hasDone = true
		}
	}
	if !hasStop {
		t.Error("Finalize after EOF should emit ContentStop for dangling item")
	}
	if !hasDone {
		t.Error("Finalize after EOF should emit IRStreamDone")
	}
}
