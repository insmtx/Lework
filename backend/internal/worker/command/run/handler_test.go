package run

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/assistant"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/pkg/seqtracker"
)

type handlerPublisher struct {
	mu     sync.Mutex
	events []messaging.RunEvent
}

func (p *handlerPublisher) Publish(_ context.Context, _ string, value any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if event, ok := value.(messaging.RunEvent); ok {
		p.events = append(p.events, event)
	}
	return nil
}

func (*handlerPublisher) Request(context.Context, string, any) (*nats.Msg, error) {
	return nil, nil
}

type handlerPreparer struct{}

func (handlerPreparer) Prepare(
	_ context.Context,
	req *assistantdomain.RunRequest,
) (*assistant.PreparedRun, error) {
	return &assistant.PreparedRun{
		Request: req,
		Execution: agent.ExecutionRequest{
			ExecutionID: req.RunID,
			TraceID:     req.TraceID,
			Runtime:     "test",
		},
	}, nil
}

type handlerFinalizer struct{}

func (handlerFinalizer) FinalizeRequired(
	_ context.Context,
	run *assistant.PreparedRun,
	runtimeResult *agent.ExecutionResult,
	_ assistant.JournalSnapshot,
) (*assistant.Finalization, error) {
	return &assistant.Finalization{Result: &assistantdomain.RunResult{
		RunID:   run.Request.RunID,
		TraceID: run.Request.TraceID,
		Status:  assistantdomain.RunStatusCompleted,
		Message: runtimeResult.Message,
	}}, nil
}

func (handlerFinalizer) PostRunBestEffort(
	context.Context,
	*assistant.PreparedRun,
	*assistantdomain.RunResult,
	assistant.JournalSnapshot,
) {
}

type handlerRuntime struct {
	started chan struct{}
	release chan struct{}
	err     error
}

type trackerCall struct {
	seq    uint64
	status seqtracker.Status
}

type trackerRecorder struct {
	mu    sync.Mutex
	calls []trackerCall
}

func (t *trackerRecorder) TrackReceived(
	context.Context,
	string,
	uint64,
	string,
	string,
	string,
	string,
) error {
	return nil
}

func (t *trackerRecorder) MarkProcessing(_ context.Context, _ string, seq uint64) error {
	t.record(seq, seqtracker.StatusProcessing)
	return nil
}

func (t *trackerRecorder) MarkCompleted(_ context.Context, _ string, seq uint64) error {
	t.record(seq, seqtracker.StatusCompleted)
	return nil
}

func (t *trackerRecorder) MarkFailed(_ context.Context, _ string, seq uint64, _ string) error {
	t.record(seq, seqtracker.StatusFailed)
	return nil
}

func (*trackerRecorder) GetLastCompletedSeq(context.Context, string) (uint64, error) {
	return 0, nil
}

func (*trackerRecorder) GetLastTerminalSeq(context.Context, string) (uint64, error) {
	return 0, nil
}

func (*trackerRecorder) IsDuplicate(context.Context, string, uint64) (bool, error) {
	return false, nil
}

func (*trackerRecorder) IsTerminal(context.Context, string, uint64) (bool, error) {
	return false, nil
}

func (*trackerRecorder) Close() error { return nil }

func (t *trackerRecorder) record(seq uint64, status seqtracker.Status) {
	t.mu.Lock()
	t.calls = append(t.calls, trackerCall{seq: seq, status: status})
	t.mu.Unlock()
}

func (t *trackerRecorder) statuses() []trackerCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]trackerCall(nil), t.calls...)
}

func (*handlerRuntime) Name() string { return "test" }

func (r *handlerRuntime) Execute(
	context.Context,
	agent.ExecutionRequest,
	agent.Observer,
) (agent.ExecutionResult, error) {
	close(r.started)
	<-r.release
	if r.err != nil {
		return agent.ExecutionResult{}, r.err
	}
	return agent.ExecutionResult{Message: "done"}, nil
}

func TestHandlerWaitsForExecutionAndPublishesReplyMessageIDs(t *testing.T) {
	runtime := &handlerRuntime{started: make(chan struct{}), release: make(chan struct{})}
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	service := assistant.NewService(
		handlerPreparer{},
		agent.NewExecutor(registry),
		handlerFinalizer{},
		assistant.NewJournalFactory(),
	)
	publisher := &handlerPublisher{}
	handler, err := New(Config{
		OrgID:          1,
		WorkerID:       2,
		MaxConcurrency: 1,
		DebounceWindow: 5 * time.Millisecond,
	}, publisher, service)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tracker := &trackerRecorder{}
	handler.seqTracker = tracker
	defer handler.Close()

	command := messaging.NewRunCommand(
		"message-1",
		messaging.RouteContext{OrgID: 1, WorkerID: 2, SessionID: "session-1"},
		messaging.TraceContext{
			TraceID:   "trace-1",
			RequestID: "request-1",
			TaskID:    "task-1",
			RunID:     "run-1",
		},
		messaging.RunCommandPayload{
			TaskType: messaging.TaskTypeAgentRun,
			Execution: messaging.ExecutionTarget{
				AssistantID: "assistant-1",
			},
			Input: messaging.TaskInput{
				Type: messaging.InputTypeMessage,
				Messages: []messaging.ChatMessage{
					{ID: "user-message-1", Role: messaging.MessageRoleUser, Content: "first"},
					{ID: "user-message-1", Role: messaging.MessageRoleUser, Content: "duplicate"},
					{ID: "user-message-2", Role: messaging.MessageRoleUser, Content: "second"},
				},
			},
			Model: messaging.ModelOptions{
				Provider: "openai",
				Model:    "test-model",
				APIKey:   "test-key",
			},
			Runtime: messaging.RuntimeOptions{Kind: "test"},
		},
		nil,
	)

	completed := make(chan error, 1)
	go func() {
		completed <- handler.HandleRunCommand(context.Background(), command, &nats.Msg{
			Reply: "$JS.ACK.stream.consumer.1.42.1.123456789.0",
			Sub:   &nats.Subscription{},
		})
	}()
	<-runtime.started
	if calls := tracker.statuses(); len(calls) != 1 ||
		calls[0].seq != 42 ||
		calls[0].status != seqtracker.StatusProcessing {
		t.Fatalf("statuses before completion = %#v", calls)
	}
	select {
	case err := <-completed:
		t.Fatalf("handler returned before runtime completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(runtime.release)
	if err := <-completed; err != nil {
		t.Fatalf("HandleRunCommand() error = %v", err)
	}
	if calls := tracker.statuses(); len(calls) != 2 ||
		calls[1].seq != 42 ||
		calls[1].status != seqtracker.StatusCompleted {
		t.Fatalf("statuses after completion = %#v", calls)
	}

	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	var terminal *messaging.RunEvent
	for i := range publisher.events {
		if publisher.events[i].Body.Event == messaging.RunEventRunCompleted {
			terminal = &publisher.events[i]
			break
		}
	}
	if terminal == nil {
		t.Fatalf("published events = %#v", publisher.events)
	}
	replyIDs := terminal.Body.ReplyToMessageIDs
	if len(replyIDs) != 2 || replyIDs[0] != "user-message-1" || replyIDs[1] != "user-message-2" {
		t.Fatalf("reply ids = %v", replyIDs)
	}
}

func TestHandlerMarksDeliveryFailedAfterExecutionFailure(t *testing.T) {
	runtimeErr := errors.New("runtime failed")
	runtime := &handlerRuntime{
		started: make(chan struct{}),
		release: make(chan struct{}),
		err:     runtimeErr,
	}
	close(runtime.release)
	registry := agent.NewRegistry()
	registry.Register("test", runtime)
	registry.SetDefault("test")
	service := assistant.NewService(
		handlerPreparer{},
		agent.NewExecutor(registry),
		handlerFinalizer{},
		assistant.NewJournalFactory(),
	)
	handler, err := New(Config{
		OrgID:          1,
		WorkerID:       2,
		MaxConcurrency: 1,
		DebounceWindow: time.Millisecond,
	}, &handlerPublisher{}, service)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tracker := &trackerRecorder{}
	handler.seqTracker = tracker
	defer handler.Close()

	command := messaging.NewRunCommand(
		"message-failed",
		messaging.RouteContext{OrgID: 1, WorkerID: 2, SessionID: "session-1"},
		messaging.TraceContext{TraceID: "trace-1", TaskID: "task-1", RunID: "run-failed"},
		messaging.RunCommandPayload{
			TaskType:  messaging.TaskTypeAgentRun,
			Execution: messaging.ExecutionTarget{AssistantID: "assistant-1"},
			Input: messaging.TaskInput{
				Type:     messaging.InputTypeMessage,
				Messages: []messaging.ChatMessage{{ID: "user-message-1", Role: messaging.MessageRoleUser, Content: "fail"}},
			},
			Model:   messaging.ModelOptions{Provider: "openai", Model: "test-model", APIKey: "test-key"},
			Runtime: messaging.RuntimeOptions{Kind: "test"},
		},
		nil,
	)
	err = handler.HandleRunCommand(context.Background(), command, &nats.Msg{
		Reply: "$JS.ACK.stream.consumer.1.43.1.123456789.0",
		Sub:   &nats.Subscription{},
	})
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("HandleRunCommand() error = %v, want runtime error", err)
	}
	calls := tracker.statuses()
	if len(calls) != 2 ||
		calls[0] != (trackerCall{seq: 43, status: seqtracker.StatusProcessing}) ||
		calls[1] != (trackerCall{seq: 43, status: seqtracker.StatusFailed}) {
		t.Fatalf("tracker calls = %#v", calls)
	}
}
