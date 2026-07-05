package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestDriverReturnsInvocationResult(t *testing.T) {
	invoker := &fakeInvoker{
		result: InvocationResult{
			Message: "done",
			Usage: &agent.Usage{
				InputTokens:  12,
				OutputTokens: 5,
				TotalTokens:  17,
			},
			ProviderSessionID: "provider-session-1",
		},
	}
	driver := newTestDriver(t, invoker)

	result, err := driver.RunInvocation(context.Background(), testExecutionRequest(), nil)
	if err != nil {
		t.Fatalf("RunInvocation() error = %v", err)
	}
	if result.Message != "done" {
		t.Fatalf("Message = %q, want done", result.Message)
	}
	if result.Usage == nil || result.Usage.TotalTokens != 17 {
		t.Fatalf("Usage = %#v, want total 17", result.Usage)
	}
	if result.ProviderConversationID != "provider-session-1" {
		t.Fatalf("ProviderConversationID = %q", result.ProviderConversationID)
	}
}

func TestDriverPassesPreparedProviderSession(t *testing.T) {
	invoker := &fakeInvoker{result: InvocationResult{Message: "done"}}
	driver := newTestDriver(t, invoker)
	request := testExecutionRequest()
	request.ProviderSession = agent.ProviderSession{ID: "resume-1", Resume: true}

	if _, err := driver.RunInvocation(context.Background(), request, nil); err != nil {
		t.Fatalf("RunInvocation() error = %v", err)
	}
	if invoker.request.SessionID != "resume-1" || !invoker.request.Resume {
		t.Fatalf("InvocationRequest session = %q resume=%v", invoker.request.SessionID, invoker.request.Resume)
	}
}

func TestDriverForwardsStronglyTypedNodeEvents(t *testing.T) {
	invoker := &fakeInvoker{
		events: []agent.NodeEvent{
			agent.NewAgentStartEvent("session-1"),
			agent.NewMessageUpdateEvent("provider-message-1", "hello"),
			agent.NewToolExecutionStartEvent("call-1", "Bash", agent.MarshalRawJSON(map[string]string{"command": "date"})),
			agent.NewToolExecutionEndEvent("call-1", "Bash", agent.MarshalRawJSON("done"), 4),
			agent.NewMessageEndEvent("hello", &agent.Usage{InputTokens: 1, OutputTokens: 2}),
		},
		result: InvocationResult{
			Message:           "hello",
			Usage:             &agent.Usage{InputTokens: 1, OutputTokens: 2},
			ProviderSessionID: "session-1",
		},
	}
	driver := newTestDriver(t, invoker)

	var observed []agent.NodeEvent
	observer := agent.NodeObserverFunc(func(_ context.Context, event agent.NodeEvent) error {
		observed = append(observed, event)
		return nil
	})
	if _, err := driver.RunInvocation(context.Background(), testExecutionRequest(), observer); err != nil {
		t.Fatalf("RunInvocation() error = %v", err)
	}

	assertNodePayload(t, observed, agent.NodeEventAgentStart, (*agent.AgentStartedPayload)(nil))
	assertNodePayload(t, observed, agent.NodeEventMessageUpdate, (*agent.MessageUpdatePayload)(nil))
	assertNodePayload(t, observed, agent.NodeEventToolExecutionStart, (*agent.ToolExecutionStartPayload)(nil))
	assertNodePayload(t, observed, agent.NodeEventToolExecutionEnd, (*agent.ToolExecutionEndPayload)(nil))
	assertNodePayload(t, observed, agent.NodeEventMessageEnd, (*agent.MessageEndPayload)(nil))
	for _, event := range observed {
		if event.ExecutionID != "run-1" || event.TraceID != "trace-1" {
			t.Fatalf("event context = execution %q trace %q", event.ExecutionID, event.TraceID)
		}
		if len(event.Metadata) != 0 {
			t.Fatalf("unexpected NodeEvent metadata: %#v", event.Metadata)
		}
	}
}

func TestDriverNormalizesTodoThroughNodeObserver(t *testing.T) {
	invoker := &fakeInvoker{
		events: []agent.NodeEvent{
			agent.NewTodoUpdatedEvent([]agent.RuntimeTodoItem{{Title: "Inspect", Status: "unknown"}}),
		},
		result: InvocationResult{Message: "done"},
	}
	driver := newTestDriver(t, invoker)

	var todo *agent.TodoUpdatedPayload
	observer := agent.NodeObserverFunc(func(_ context.Context, event agent.NodeEvent) error {
		if event.Type == agent.NodeEventTodoUpdated {
			switch payload := event.Payload.(type) {
			case *agent.TodoUpdatedPayload:
				todo = payload
			case agent.TodoUpdatedPayload:
				todo = &payload
			}
		}
		return nil
	})
	if _, err := driver.RunInvocation(context.Background(), testExecutionRequest(), observer); err != nil {
		t.Fatalf("RunInvocation() error = %v", err)
	}
	if todo == nil || len(todo.Items) != 1 {
		t.Fatalf("todo payload = %#v", todo)
	}
	if todo.Items[0].ID == "" || todo.Items[0].Status != "pending" {
		t.Fatalf("normalized todo = %#v", todo.Items[0])
	}
}

func TestDriverReturnsTerminalErrorWithoutLifecycleNodeEvent(t *testing.T) {
	expected := errors.New("provider failed")
	invoker := &fakeInvoker{
		events: []agent.NodeEvent{agent.NewMessageUpdateEvent("m1", "partial")},
		result: InvocationResult{Message: "partial", Err: expected},
	}
	driver := newTestDriver(t, invoker)

	var observed []agent.NodeEvent
	_, err := driver.RunInvocation(context.Background(), testExecutionRequest(), agent.NodeObserverFunc(
		func(_ context.Context, event agent.NodeEvent) error {
			observed = append(observed, event)
			return nil
		},
	))
	if !errors.Is(err, expected) {
		t.Fatalf("RunInvocation() error = %v, want %v", err, expected)
	}
	if len(observed) != 1 || observed[0].Type != agent.NodeEventMessageUpdate {
		t.Fatalf("public NodeEvents = %#v, want only provider message delta", observed)
	}
}

func newTestDriver(t *testing.T, invoker Invoker) *Driver {
	t.Helper()
	driver, err := NewDriver("fake", invoker)
	if err != nil {
		t.Fatalf("NewDriver() error = %v", err)
	}
	return driver
}

func testExecutionRequest() agent.ExecutionRequest {
	return agent.ExecutionRequest{
		ExecutionID: "run-1",
		TraceID:     "trace-1",
		Runtime:     "fake",
		Prompt:      "hello",
		Model: agent.ModelConfig{
			Provider: "openai",
			Model:    "test",
			APIKey:   "test-key",
		},
		Filesystem: agent.FilesystemContext{WorkDir: "/tmp"},
	}
}

type fakeInvoker struct {
	request InvocationRequest
	events  []agent.NodeEvent
	result  InvocationResult
}

func (f *fakeInvoker) Prepare(context.Context, string) error {
	return nil
}

func (f *fakeInvoker) Invoke(_ context.Context, request InvocationRequest) (*Invocation, error) {
	f.request = request
	events := make(chan agent.NodeEvent, len(f.events))
	for _, event := range f.events {
		events <- event
	}
	close(events)
	result := make(chan InvocationResult, 1)
	result <- f.result
	close(result)
	return &Invocation{Events: events, Result: result}, nil
}

func assertNodePayload(t *testing.T, events []agent.NodeEvent, eventType agent.NodeEventType, expected any) {
	t.Helper()
	for _, event := range events {
		if event.Type != eventType {
			continue
		}
		if reflect.TypeOf(event.Payload) != reflect.TypeOf(expected) {
			t.Fatalf("%s payload type = %T", eventType, event.Payload)
		}
		return
	}
	t.Fatalf("missing event %s", eventType)
}
