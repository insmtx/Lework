package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	clauderuntime "github.com/insmtx/Leros/backend/agent/runtime/claude"
	codexruntime "github.com/insmtx/Leros/backend/agent/runtime/codex"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/agent/runtime/externalcli"
	nativeruntime "github.com/insmtx/Leros/backend/agent/runtime/native"
	opencoderuntime "github.com/insmtx/Leros/backend/agent/runtime/opencode"
)

type contractBackend struct {
	executionRequest  agent.ExecutionRequest
	invocationRequest externalcli.InvocationRequest
	err               error
}

func (*contractBackend) Prepare(context.Context, string) error { return nil }

func (b *contractBackend) Invoke(
	_ context.Context,
	request externalcli.InvocationRequest,
) (*externalcli.Invocation, error) {
	b.invocationRequest = request
	if b.err != nil {
		return nil, b.err
	}
	eventChannel := make(chan agent.Event, 3)
	eventChannel <- *events.NewMessageDelta("provider-message-1", "done")
	eventChannel <- agent.Event{Type: events.EventResult, Content: "done"}
	eventChannel <- agent.Event{Type: events.EventInvocationCompleted}
	close(eventChannel)
	return &externalcli.Invocation{Events: eventChannel}, nil
}

func (b *contractBackend) Execute(
	_ context.Context,
	request agent.ExecutionRequest,
	observer agent.Observer,
) (agent.ExecutionResult, error) {
	b.executionRequest = request
	if b.err != nil {
		return agent.ExecutionResult{}, b.err
	}
	if observer != nil {
		if err := observer.Emit(context.Background(), events.NewMessageDelta("native-message-1", "done")); err != nil {
			return agent.ExecutionResult{}, err
		}
	}
	return agent.ExecutionResult{Message: "done"}, nil
}

type contractObserver struct {
	events []agent.Event
	err    error
}

func (o *contractObserver) Emit(_ context.Context, event *agent.Event) error {
	if event != nil {
		o.events = append(o.events, *event)
	}
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
			return clauderuntime.NewWithInvoker(backend, externalcli.DriverOptions{})
		}},
		{name: codexruntime.Kind, new: func(backend *contractBackend) (agent.Runtime, error) {
			return codexruntime.NewWithInvoker(backend, externalcli.DriverOptions{})
		}},
		{name: opencoderuntime.Kind, new: func(backend *contractBackend) (agent.Runtime, error) {
			return opencoderuntime.NewWithInvoker(backend, externalcli.DriverOptions{})
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
			if len(observer.events) != 1 || observer.events[0].Type != events.EventMessageDelta {
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
