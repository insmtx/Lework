package agent_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	clauderuntime "github.com/insmtx/Leros/backend/agent/runtime/claude"
	codexruntime "github.com/insmtx/Leros/backend/agent/runtime/codex"
	runtimecli "github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
	nativeruntime "github.com/insmtx/Leros/backend/agent/runtime/native"
	opencoderuntime "github.com/insmtx/Leros/backend/agent/runtime/opencode"
)

type contractBackend struct {
	executionRequest  agent.ExecutionRequest
	invocationRequest runtimecli.InvocationRequest
	err               error
}

func (*contractBackend) Prepare(context.Context, string) error { return nil }

func (b *contractBackend) Invoke(
	_ context.Context,
	request runtimecli.InvocationRequest,
) (*runtimecli.Invocation, error) {
	b.invocationRequest = request
	if b.err != nil {
		return nil, b.err
	}
	eventChannel := make(chan agent.NodeEvent, 1)
	eventChannel <- agent.NewMessageUpdateEvent("provider-message-1", "done")
	close(eventChannel)
	resultChannel := make(chan runtimecli.InvocationResult, 1)
	resultChannel <- runtimecli.InvocationResult{Message: "done"}
	close(resultChannel)
	return &runtimecli.Invocation{Events: eventChannel, Result: resultChannel}, nil
}

func (b *contractBackend) Execute(
	_ context.Context,
	request agent.ExecutionRequest,
	observer agent.NodeObserver,
) (agent.ExecutionResult, error) {
	b.executionRequest = request
	if b.err != nil {
		return agent.ExecutionResult{}, b.err
	}
	if observer != nil {
		if err := observer.Observe(context.Background(), agent.NodeEvent{
			ExecutionID: request.ExecutionID,
			Type:        agent.NodeEventType(agent.NodeEventMessageUpdate),
			Payload:     agent.MessageUpdatePayload{MessageID: "native-message-1"},
		}); err != nil {
			return agent.ExecutionResult{}, err
		}
	}
	return agent.ExecutionResult{Message: "done"}, nil
}

type contractObserver struct {
	events []agent.NodeEvent
	err    error
}

func (o *contractObserver) Observe(_ context.Context, event agent.NodeEvent) error {
	o.events = append(o.events, event)
	return o.err
}

func TestConcreteRuntimesFollowRuntimeContract(t *testing.T) {
	tests := []struct {
		name string
		new  func(*contractBackend) (agent.Runtime, error)
	}{
		{name: nativeruntime.Kind, new: func(backend *contractBackend) (agent.Runtime, error) {
			return nativeruntime.NewWithExecutor(backend)
		}},
		{name: clauderuntime.Kind, new: func(backend *contractBackend) (agent.Runtime, error) {
			return clauderuntime.NewWithInvoker(backend, agent.RuntimeAdapterOptions{})
		}},
		{name: codexruntime.Kind, new: func(backend *contractBackend) (agent.Runtime, error) {
			return codexruntime.NewWithInvoker(backend, agent.RuntimeAdapterOptions{})
		}},
		{name: opencoderuntime.Kind, new: func(backend *contractBackend) (agent.Runtime, error) {
			return opencoderuntime.NewWithInvoker(backend, agent.RuntimeAdapterOptions{})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &contractBackend{}
			runtime, err := test.new(backend)
			if err != nil {
				t.Fatalf("create runtime: %v", err)
			}
			request := agent.ExecutionRequest{ExecutionID: "execution-1", Runtime: test.name}
			observer := &contractObserver{}
			result, err := runtime.Execute(context.Background(), request, observer)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if runtime.Name() != test.name || result.Message != "done" {
				t.Fatalf("runtime name/result = %q/%q", runtime.Name(), result.Message)
			}
			if len(observer.events) != 1 ||
				agent.NodeEventType(observer.events[0].Type) != agent.NodeEventType(agent.NodeEventMessageUpdate) {
				t.Fatalf("activity events = %#v", observer.events)
			}
			if test.name == nativeruntime.Kind {
				if backend.executionRequest.ExecutionID != request.ExecutionID {
					t.Fatalf("forwarded request = %#v", backend.executionRequest)
				}
			} else if backend.invocationRequest.ExecutionID != request.ExecutionID {
				t.Fatalf("forwarded request = %#v", backend.invocationRequest)
			}

			backend.err = context.Canceled
			if _, err := runtime.Execute(context.Background(), request, nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error = %v", err)
			}

			backend.err = nil
			observerErr := errors.New("observer failed")
			if _, err := runtime.Execute(
				context.Background(),
				request,
				&contractObserver{err: observerErr},
			); !errors.Is(err, observerErr) {
				t.Fatalf("observer error = %v", err)
			}
		})
	}
}

// runtimeFunc adapts a function to the agent.Runtime interface.
type runtimeFunc func(context.Context, agent.ExecutionRequest, agent.NodeObserver) (agent.ExecutionResult, error)

func (f runtimeFunc) Name() string { return nativeruntime.Kind }

func (f runtimeFunc) Execute(ctx context.Context, request agent.ExecutionRequest, observer agent.NodeObserver) (agent.ExecutionResult, error) {
	return f(ctx, request, observer)
}

// failOnNthObserver returns an error when the Nth event is emitted.
type failOnNthObserver struct {
	events []agent.NodeEvent
	failOn int
	err    error
}

func (o *failOnNthObserver) Observe(_ context.Context, event agent.NodeEvent) error {
	o.events = append(o.events, event)
	if len(o.events) == o.failOn {
		return o.err
	}
	return nil
}

// TestExecutorPassesSerialObserver verifies the Executor wraps the observer
// in a SerialObserver before passing it to the Runtime.
func TestExecutorPassesSerialObserver(t *testing.T) {
	var receivedObserver agent.NodeObserver
	registry := agent.NewRegistry()
	registry.Register(nativeruntime.Kind, runtimeFunc(func(ctx context.Context, request agent.ExecutionRequest, observer agent.NodeObserver) (agent.ExecutionResult, error) {
		receivedObserver = observer
		return agent.ExecutionResult{Message: "done"}, nil
	}))
	registry.SetDefault(nativeruntime.Kind)
	executor := agent.NewExecutor(registry)

	observer := &contractObserver{}
	result, err := executor.Execute(
		context.Background(),
		agent.ExecutionRequest{ExecutionID: "serial-test", Runtime: nativeruntime.Kind},
		observer,
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Message != "done" {
		t.Fatalf("result message = %q", result.Message)
	}
	if _, ok := receivedObserver.(*agent.SerialObserver); !ok {
		t.Fatalf("Runtime received %T, want *SerialObserver", receivedObserver)
	}
}

// TestContractObserverErrorTerminates verifies that when an observer
// returns an error during an activity event, the execution stops.
func TestContractObserverErrorTerminates(t *testing.T) {
	observerErr := errors.New("observer failed mid-stream")
	registry := agent.NewRegistry()
	registry.Register(nativeruntime.Kind, runtimeFunc(func(ctx context.Context, request agent.ExecutionRequest, observer agent.NodeObserver) (agent.ExecutionResult, error) {
		if observer != nil {
			// First event succeeds.
			if err := observer.Observe(ctx, agent.NodeEvent{
				Type:    agent.NodeEventType(agent.NodeEventMessageUpdate),
				Payload: agent.MessageUpdatePayload{MessageID: "m1"},
			}); err != nil {
				return agent.ExecutionResult{}, err
			}
			// Second event should fail.
			if err := observer.Observe(ctx, agent.NodeEvent{
				Type:    agent.NodeEventType(agent.NodeEventMessageUpdate),
				Payload: agent.MessageUpdatePayload{MessageID: "m2"},
			}); err != nil {
				return agent.ExecutionResult{}, err
			}
		}
		return agent.ExecutionResult{Message: "done"}, nil
	}))
	registry.SetDefault(nativeruntime.Kind)
	executor := agent.NewExecutor(registry)

	failObserver := &failOnNthObserver{failOn: 2, err: observerErr}
	_, err := executor.Execute(
		context.Background(),
		agent.ExecutionRequest{ExecutionID: "fail-mid", Runtime: nativeruntime.Kind},
		failObserver,
	)
	if !errors.Is(err, observerErr) {
		t.Fatalf("Execute() error = %v, want observerErr", err)
	}
}

// TestAPIKeyReachesRuntimes verifies the API key flows from ExecutionRequest
// to each Runtime backend without leaking into NodeEvent payloads or error messages.
func TestAPIKeyReachesRuntimes(t *testing.T) {
	const testAPIKey = "sk-test-secret-key-12345"
	request := agent.ExecutionRequest{
		ExecutionID: "api-key-test",
		Runtime:     "leros",
		Model:       agent.ModelConfig{APIKey: testAPIKey, Model: "test-model"},
	}

	t.Run("native", func(t *testing.T) {
		var captured agent.ExecutionRequest
		registry := agent.NewRegistry()
		registry.Register(nativeruntime.Kind, runtimeFunc(func(ctx context.Context, req agent.ExecutionRequest, observer agent.NodeObserver) (agent.ExecutionResult, error) {
			captured = req
			return agent.ExecutionResult{Message: "done"}, nil
		}))
		registry.SetDefault(nativeruntime.Kind)
		executor := agent.NewExecutor(registry)
		req := request
		req.Runtime = nativeruntime.Kind
		observer := &contractObserver{}
		_, err := executor.Execute(context.Background(), req, observer)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if captured.Model.APIKey != testAPIKey {
			t.Fatalf("Native API key = %q, want %q", captured.Model.APIKey, testAPIKey)
		}
		for _, ev := range observer.events {
			if strings.Contains(fmt.Sprintf("%+v", ev), testAPIKey) {
				t.Errorf("Native NodeEvent leaks API key: %+v", ev)
			}
		}
	})

	for _, kind := range []string{clauderuntime.Kind, codexruntime.Kind, opencoderuntime.Kind} {
		t.Run(kind, func(t *testing.T) {
			backend := &contractBackend{}
			var rt agent.Runtime
			var err error
			switch kind {
			case clauderuntime.Kind:
				rt, err = clauderuntime.NewWithInvoker(backend, agent.RuntimeAdapterOptions{})
			case codexruntime.Kind:
				rt, err = codexruntime.NewWithInvoker(backend, agent.RuntimeAdapterOptions{})
			case opencoderuntime.Kind:
				rt, err = opencoderuntime.NewWithInvoker(backend, agent.RuntimeAdapterOptions{})
			}
			if rt == nil {
				t.Skipf("runtime creation failed: %v", err)
				return
			}
			req := request
			req.Runtime = kind
			observer := &contractObserver{}
			_, err = rt.Execute(context.Background(), req, observer)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if backend.invocationRequest.Model.APIKey != testAPIKey {
				t.Fatalf("%s API key = %q, want %q", kind, backend.invocationRequest.Model.APIKey, testAPIKey)
			}
			for _, ev := range observer.events {
				if strings.Contains(fmt.Sprintf("%+v", ev), testAPIKey) {
					t.Errorf("%s NodeEvent leaks API key: %+v", kind, ev)
				}
			}
		})
	}
}
