package externalcli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestRunnerAdaptsEngineResult(t *testing.T) {
	engine := &fakeInvoker{
		events: []agent.Event{
			{Type: events.EventInvocationStarted},
			*events.NewMessageResult("done", &agent.Usage{
				InputTokens:  12,
				OutputTokens: 5,
				TotalTokens:  17,
			}),
			{Type: events.EventInvocationCompleted},
		},
	}
	runner, err := NewDriver("fake", engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	result, err := runTestRequest(runner, &assistantdomain.RunRequest{
		RunID:        "run_cli",
		SystemPrompt: "system only",
		Input: assistantdomain.InputContext{
			Type:     assistantdomain.InputTypeMessage,
			Messages: []assistantdomain.InputMessage{{Role: "user", Content: "hello"}},
		},
		Runtime: assistantdomain.RuntimeOptions{WorkDir: "/tmp"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
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

func TestRunnerDefaultsEmptyWorkDirToWorkspaceTemp(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	engine := &fakeInvoker{result: "done"}
	runner, err := NewDriver("fake", engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	expected := filepath.Join(workspaceRoot, "temp")
	_, err = runTestRequest(runner, &assistantdomain.RunRequest{
		RunID: "run_temp",
		Input: assistantdomain.InputContext{
			Type:     assistantdomain.InputTypeMessage,
			Messages: []assistantdomain.InputMessage{{Role: "user", Content: "hello"}},
		},
		Runtime: assistantdomain.RuntimeOptions{WorkDir: expected},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if engine.runReq.WorkDir != expected {
		t.Fatalf("work dir = %q, want %q", engine.runReq.WorkDir, expected)
	}
}

func TestRunnerStoresProviderSessionAndResumes(t *testing.T) {
	store := NewInMemoryProviderSessionStore()
	engine := &fakeInvoker{
		result:            "done",
		providerSessionID: "provider-session-1",
	}
	runner, err := NewDriver("codex", engine, DriverOptions{SessionStore: store})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}
	req := &assistantdomain.RunRequest{
		RunID: "run_first",
		Conversation: assistantdomain.ConversationContext{
			ID: "internal-session-1",
		},
		Assistant: assistantdomain.AssistantContext{
			ID: "assistant-1",
		},
		Input: assistantdomain.InputContext{
			Type:     assistantdomain.InputTypeMessage,
			Messages: []assistantdomain.InputMessage{{Role: "user", Content: "hello"}},
		},
		Runtime: assistantdomain.RuntimeOptions{WorkDir: "/tmp"},
	}

	if _, err := runTestRequest(runner, req); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if engine.runReq.Resume {
		t.Fatal("first run should not resume")
	}
	if engine.runReq.SessionID != "" {
		t.Fatalf("first codex run should not preallocate provider session, got %q", engine.runReq.SessionID)
	}

	req.RunID = "run_second"
	if _, err := runTestRequest(runner, req); err != nil {
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
	engine := &fakeInvoker{result: "done"}
	runner, err := NewDriver("claude", engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	_, err = runTestRequest(runner, &assistantdomain.RunRequest{
		RunID: "run_claude",
		Conversation: assistantdomain.ConversationContext{
			ID: "internal-session-claude",
		},
		Input: assistantdomain.InputContext{
			Type:     assistantdomain.InputTypeMessage,
			Messages: []assistantdomain.InputMessage{{Role: "user", Content: "hello"}},
		},
		Runtime: assistantdomain.RuntimeOptions{WorkDir: "/tmp"},
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
	engine := &fakeInvoker{
		events: []agent.Event{
			{Type: events.EventInvocationStarted},
			{Type: events.EventToolCallStarted, Content: `{"call_id":"call_123","name":"Bash","arguments":{"command":"date"}}`},
			{Type: events.EventToolCallCompleted, Content: `{"tool_call_id":"call_123","name":"Bash","result":"Thu May 14 14:19:24 CST 2026","is_error":false}`},
			{Type: events.EventResult, Content: "done"},
			{Type: events.EventInvocationCompleted},
		},
	}
	runner, err := NewDriver("claude", engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	var emitted []agent.Event
	sink := events.SinkFunc(func(_ context.Context, event *agent.Event) error {
		emitted = append(emitted, *event)
		return nil
	})
	result, err := runTestRequest(runner, &assistantdomain.RunRequest{
		RunID:     "run_tool_events",
		TraceID:   "trace_tool_events",
		EventSink: sink,
		Input: assistantdomain.InputContext{
			Type:     assistantdomain.InputTypeMessage,
			Messages: []assistantdomain.InputMessage{{Role: "user", Content: "hello"}},
		},
		Runtime: assistantdomain.RuntimeOptions{WorkDir: "/tmp"},
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
	engine := &fakeInvoker{
		events: []agent.Event{
			{Type: events.EventInvocationStarted},
			*events.NewTodoUpdated([]events.RuntimeTodoItem{{Title: "Inspect", Status: "unknown"}}),
			{Type: events.EventResult, Content: "done"},
			{Type: events.EventInvocationCompleted},
		},
	}
	runner, err := NewDriver("codex", engine)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	var emitted []agent.Event
	sink := events.SinkFunc(func(_ context.Context, event *agent.Event) error {
		emitted = append(emitted, *event)
		return nil
	})
	_, err = runTestRequest(runner, &assistantdomain.RunRequest{
		RunID:     "run_todo",
		TraceID:   "trace_todo",
		EventSink: sink,
		Input: assistantdomain.InputContext{
			Type:     assistantdomain.InputTypeMessage,
			Messages: []assistantdomain.InputMessage{{Role: "user", Content: "hello"}},
		},
		Runtime: assistantdomain.RuntimeOptions{WorkDir: "/tmp"},
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

func runTestRequest(runner *Driver, request *assistantdomain.RunRequest) (agent.ExecutionResult, error) {
	prompt := assistantdomain.BuildUserInput(request)
	return runner.RunInvocation(context.Background(), agent.ExecutionRequest{
		ExecutionID:  request.RunID,
		TraceID:      request.TraceID,
		Runtime:      runner.name,
		SessionKey:   request.Conversation.ID,
		InstanceKey:  request.Assistant.ID,
		SystemPrompt: request.SystemPrompt,
		Prompt:       prompt,
		Model: agent.ModelConfig{
			Provider: request.Model.Provider,
			Model:    request.Model.Model,
			APIKey:   request.Model.APIKey,
			BaseURL:  request.Model.BaseURL,
		},
		Policy: agent.ExecutionPolicy{PermissionMode: request.Policy.PermissionMode},
		Filesystem: agent.FilesystemContext{
			WorkDir: request.Runtime.WorkDir,
			RepoDir: request.Workspace.RepoDir,
		},
	}, request.EventSink)
}

type fakeInvoker struct {
	runReq            InvocationRequest
	result            string
	providerSessionID string
	events            []agent.Event
}

func (e *fakeInvoker) Prepare(_ context.Context, _ string) error {
	return nil
}

func (e *fakeInvoker) Invoke(_ context.Context, req InvocationRequest) (*Invocation, error) {
	e.runReq = req
	eventList := e.events
	if len(eventList) == 0 {
		eventList = []agent.Event{
			{Type: events.EventInvocationStarted},
			{Type: events.EventResult, Content: e.result},
			{Type: events.EventInvocationCompleted},
		}
	}
	eventChan := make(chan agent.Event, len(eventList)+1)
	if e.providerSessionID != "" {
		eventChan <- agent.Event{Type: events.EventProviderSessionStarted, Content: e.providerSessionID}
	}
	for _, event := range eventList {
		eventChan <- event
	}
	close(eventChan)

	return &Invocation{
		Events: eventChan,
	}, nil
}

func hasEvent(eventList []agent.Event, eventType agent.EventType) bool {
	return findEvent(eventList, eventType) != nil
}

func findEvent(eventList []agent.Event, eventType agent.EventType) *agent.Event {
	for i := range eventList {
		if eventList[i].Type == eventType {
			return &eventList[i]
		}
	}
	return nil
}
