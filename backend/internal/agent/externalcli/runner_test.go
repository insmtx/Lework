package externalcli

import (
	"context"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
)

func TestRunnerAdaptsEngineResult(t *testing.T) {
	SetDefaultProviderSessionStore(NewInMemoryProviderSessionStore())
	engine := &fakeEngine{
		events: []events.Event{
			{Type: events.EventStarted},
			*events.NewMessageResult("done", &events.UsagePayload{
				InputTokens:  12,
				OutputTokens: 5,
				TotalTokens:  17,
			}),
			{Type: events.EventCompleted},
		},
	}
	runner, err := NewRunner("fake", engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	result, err := runner.Run(context.Background(), &agent.RequestContext{
		RunID:        "run_cli",
		SystemPrompt: "system only",
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "hello",
		},
		Runtime: agent.RuntimeOptions{WorkDir: "/tmp"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != agent.RunStatusCompleted {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.Message != "done" {
		t.Fatalf("expected extracted result, got %q", result.Message)
	}
	if result.Usage == nil || result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 5 || result.Usage.TotalTokens != 17 {
		t.Fatalf("expected usage to be forwarded, got %#v", result.Usage)
	}
	if engine.runReq.WorkDir != "/tmp" {
		t.Fatalf("expected work dir /tmp, got %q", engine.runReq.WorkDir)
	}
	if engine.runReq.Prompt == "" {
		t.Fatal("expected prompt to be built")
	}
	if engine.runReq.SystemPrompt != "system only" {
		t.Fatalf("expected system prompt to be forwarded, got %q", engine.runReq.SystemPrompt)
	}
	if strings.Contains(engine.runReq.Prompt, "system only") {
		t.Fatalf("expected prompt not to contain system prompt, got %q", engine.runReq.Prompt)
	}
}

func TestRunnerStoresProviderSessionAndResumes(t *testing.T) {
	store := NewInMemoryProviderSessionStore()
	SetDefaultProviderSessionStore(store)
	engine := &fakeEngine{
		result:            "done",
		providerSessionID: "provider-session-1",
	}
	runner, err := NewRunner("codex", engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	req := &agent.RequestContext{
		RunID: "run_first",
		Conversation: agent.ConversationContext{
			ID: "internal-session-1",
		},
		Assistant: agent.AssistantContext{
			ID: "assistant-1",
		},
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "hello",
		},
		Runtime: agent.RuntimeOptions{WorkDir: "/tmp"},
	}

	if _, err := runner.Run(context.Background(), req); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if engine.runReq.Resume {
		t.Fatal("first run should not resume")
	}
	if engine.runReq.SessionID != "" {
		t.Fatalf("first codex run should not preallocate provider session, got %q", engine.runReq.SessionID)
	}

	req.RunID = "run_second"
	if _, err := runner.Run(context.Background(), req); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !engine.runReq.Resume {
		t.Fatal("second run should resume")
	}
	if engine.runReq.SessionID != "provider-session-1" {
		t.Fatalf("expected provider session id, got %q", engine.runReq.SessionID)
	}
}

func TestRunnerDoesNotPreallocateClaudeProviderSession(t *testing.T) {
	SetDefaultProviderSessionStore(NewInMemoryProviderSessionStore())
	engine := &fakeEngine{result: "done"}
	runner, err := NewRunner(engines.EngineClaude, engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	_, err = runner.Run(context.Background(), &agent.RequestContext{
		RunID: "run_claude",
		Conversation: agent.ConversationContext{
			ID: "internal-session-claude",
		},
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "hello",
		},
		Runtime: agent.RuntimeOptions{WorkDir: "/tmp"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if engine.runReq.Resume {
		t.Fatal("first claude run should not resume")
	}
	if engine.runReq.SessionID != "" {
		t.Fatalf("first claude run should use CLI-generated provider session, got %q", engine.runReq.SessionID)
	}
}

func TestRunnerForwardsExternalToolEvents(t *testing.T) {
	SetDefaultProviderSessionStore(NewInMemoryProviderSessionStore())
	engine := &fakeEngine{
		events: []events.Event{
			{Type: events.EventStarted},
			{Type: events.EventToolCallStarted, Content: `{"call_id":"call_123","name":"Bash","arguments":{"command":"date"}}`},
			{Type: events.EventToolCallCompleted, Content: `{"tool_call_id":"call_123","name":"Bash","result":"Thu May 14 14:19:24 CST 2026","is_error":false}`},
			{Type: events.EventResult, Content: "done"},
			{Type: events.EventCompleted},
		},
	}
	runner, err := NewRunner(engines.EngineClaude, engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	var emitted []events.Event
	sink := events.SinkFunc(func(_ context.Context, event *events.Event) error {
		emitted = append(emitted, *event)
		return nil
	})
	result, err := runner.Run(context.Background(), &agent.RequestContext{
		RunID:     "run_tool_events",
		TraceID:   "trace_tool_events",
		EventSink: sink,
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "hello",
		},
		Runtime: agent.RuntimeOptions{WorkDir: "/tmp"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Message != "done" {
		t.Fatalf("expected result done, got %q", result.Message)
	}
	if !hasEvent(emitted, events.EventToolCallStarted) {
		t.Fatalf("expected forwarded tool_call.started, got %#v", emitted)
	}
	if !hasEvent(emitted, events.EventToolCallCompleted) {
		t.Fatalf("expected forwarded tool_call.completed, got %#v", emitted)
	}
	started := findEvent(emitted, events.EventToolCallStarted)
	payload, err := events.DecodePayload[events.ToolCallPayload](started)
	if err != nil {
		t.Fatalf("decode forwarded tool event payload: %v", err)
	}
	if payload.ToolCallID != "call_123" || payload.Name != "Bash" {
		t.Fatalf("unexpected forwarded tool event payload: %#v", payload)
	}
}

func TestRunnerNormalizesTodoEventsThroughTracker(t *testing.T) {
	SetDefaultProviderSessionStore(NewInMemoryProviderSessionStore())
	engine := &fakeEngine{
		events: []events.Event{
			{Type: events.EventStarted},
			*events.NewTodoUpdated([]events.RuntimeTodoItem{{Title: "Inspect", Status: "unknown"}}),
			{Type: events.EventResult, Content: "done"},
			{Type: events.EventCompleted},
		},
	}
	runner, err := NewRunner(engines.EngineCodex, engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	var emitted []events.Event
	sink := events.SinkFunc(func(_ context.Context, event *events.Event) error {
		emitted = append(emitted, *event)
		return nil
	})
	_, err = runner.Run(context.Background(), &agent.RequestContext{
		RunID:     "run_todo",
		TraceID:   "trace_todo",
		EventSink: sink,
		Input: agent.InputContext{
			Type: agent.InputTypeMessage,
			Text: "hello",
		},
		Runtime: agent.RuntimeOptions{WorkDir: "/tmp"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	event := findEvent(emitted, events.EventTodoUpdated)
	if event == nil {
		t.Fatalf("expected todo.updated event, got %#v", emitted)
	}
	items, err := events.DecodePayload[[]events.RuntimeTodoItem](event)
	if err != nil {
		t.Fatalf("decode todo payload: %v", err)
	}
	if len(items) != 1 || items[0].ID == "" || items[0].Title != "Inspect" || items[0].Status != "pending" {
		t.Fatalf("unexpected normalized todo payload: %#v", items)
	}
	if event.RunID != "run_todo" || event.TraceID != "trace_todo" {
		t.Fatalf("expected run metadata, got %#v", event)
	}
}

type fakeEngine struct {
	runReq            engines.RunRequest
	result            string
	providerSessionID string
	events            []events.Event
}

func (e *fakeEngine) Prepare(_ context.Context, _ engines.PrepareRequest) error {
	return nil
}

func (e *fakeEngine) GetSkillDir() string {
	return ""
}

func (e *fakeEngine) RegisterMCP(_ context.Context, _ engines.MCPServerConfig) error {
	return nil
}

func (e *fakeEngine) Run(_ context.Context, req engines.RunRequest) (*engines.RunHandle, error) {
	e.runReq = req
	eventList := e.events
	if len(eventList) == 0 {
		eventList = []events.Event{
			{Type: events.EventStarted},
			{Type: events.EventResult, Content: e.result},
			{Type: events.EventCompleted},
		}
	}
	eventChan := make(chan events.Event, len(eventList)+1)
	if e.providerSessionID != "" {
		eventChan <- events.Event{Type: engines.EventProviderSessionStarted, Content: e.providerSessionID}
	}
	for _, event := range eventList {
		eventChan <- event
	}
	close(eventChan)
	return &engines.RunHandle{
		Events: eventChan,
	}, nil
}

func hasEvent(eventList []events.Event, eventType events.EventType) bool {
	return findEvent(eventList, eventType) != nil
}

func findEvent(eventList []events.Event, eventType events.EventType) *events.Event {
	for i := range eventList {
		if eventList[i].Type == eventType {
			return &eventList[i]
		}
	}
	return nil
}
