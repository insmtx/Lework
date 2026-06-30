package run

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

func TestCoordinatorDebounceNotifiesEverySubmission(t *testing.T) {
	var executions atomic.Int32
	var merged RunSubmission
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 2,
		DebounceWindow: 40 * time.Millisecond,
	}, func(_ context.Context, submission RunSubmission, _ agent.EventSink) (*assistantdomain.RunResult, error) {
		executions.Add(1)
		merged = submission
		return &assistantdomain.RunResult{RunID: submission.EventContext.RunID}, nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	submissions := []RunSubmission{
		testSubmission("run-1", "message-1", 11),
		testSubmission("run-1", "message-2", 12),
	}
	results := make(chan RunOutcome, len(submissions))
	errs := make(chan error, len(submissions))
	for _, submission := range submissions {
		submission := submission
		go func() {
			outcome, submitErr := coordinator.Submit(context.Background(), submission)
			results <- outcome
			errs <- submitErr
		}()
	}

	for range submissions {
		if submitErr := <-errs; submitErr != nil {
			t.Fatalf("Submit() error = %v", submitErr)
		}
		outcome := <-results
		if len(outcome.DeliverySeqs) != 2 {
			t.Fatalf("DeliverySeqs = %v, want two merged sequences", outcome.DeliverySeqs)
		}
	}
	if executions.Load() != 1 {
		t.Fatalf("executions = %d, want 1", executions.Load())
	}
	if got := len(merged.Request.Input.Messages); got != 2 {
		t.Fatalf("merged messages = %d, want 2", got)
	}
	if got := merged.EventContext.ReplyToMessageIDs; len(got) != 2 {
		t.Fatalf("merged reply IDs = %v, want two IDs", got)
	}
}

func TestMergeSubmissionsPreservesFirstExecutionMode(t *testing.T) {
	existing := testSubmission("run-1", "message-1", 11)
	existing.Request.ExecutionMode = agent.ExecutionModePlan
	incoming := testSubmission("run-1", "message-2", 12)
	incoming.Request.ExecutionMode = agent.ExecutionModeDefault

	merged := mergeSubmissions(existing, incoming)

	if merged.Request.ExecutionMode != agent.ExecutionModePlan {
		t.Fatalf("execution mode = %q, want first request mode %q", merged.Request.ExecutionMode, agent.ExecutionModePlan)
	}
}

func TestCoordinatorEnforcesMaxConcurrency(t *testing.T) {
	var current atomic.Int32
	var maximum atomic.Int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 2,
		DebounceWindow: time.Millisecond,
	}, func(_ context.Context, _ RunSubmission, _ agent.EventSink) (*assistantdomain.RunResult, error) {
		n := current.Add(1)
		for {
			previous := maximum.Load()
			if n <= previous || maximum.CompareAndSwap(previous, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		current.Add(-1)
		return &assistantdomain.RunResult{}, nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = coordinator.Submit(context.Background(), RunSubmission{})
		}()
	}
	<-started
	<-started
	select {
	case <-started:
		t.Fatal("third execution started before a concurrency slot was released")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if err := coordinator.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", maximum.Load())
	}
}

func TestCoordinatorCancelCancelsActiveRun(t *testing.T) {
	started := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: time.Millisecond,
	}, func(ctx context.Context, _ RunSubmission, _ agent.EventSink) (*assistantdomain.RunResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	result := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmission("run-cancel", "message-1", 1))
		result <- submitErr
	}()
	<-started
	if err := coordinator.Cancel(context.Background(), 1, 2, "session-1", "run-cancel"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if submitErr := <-result; !errors.Is(submitErr, context.Canceled) {
		t.Fatalf("Submit() error = %v, want context.Canceled", submitErr)
	}
}

func TestCoordinatorSerializesSameSessionAndRunsDifferentSessionsInParallel(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	otherStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 2,
		DebounceWindow: 5 * time.Millisecond,
	}, func(_ context.Context, submission RunSubmission, _ agent.EventSink) (*assistantdomain.RunResult, error) {
		message := submission.Request.Input.Messages[0].Content
		switch message {
		case "first":
			close(firstStarted)
			<-releaseFirst
		case "second":
			close(secondStarted)
		case "other":
			close(otherStarted)
		}
		return &assistantdomain.RunResult{RunID: submission.EventContext.RunID}, nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	first := testSubmission("run-first", "first", 1)
	second := testSubmission("run-second", "second", 2)
	other := testSubmission("run-other", "other", 3)
	other.EventContext.SessionID = "session-2"

	results := make(chan error, 3)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), first)
		results <- submitErr
	}()
	<-firstStarted
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), second)
		results <- submitErr
	}()
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), other)
		results <- submitErr
	}()
	<-otherStarted
	select {
	case <-secondStarted:
		t.Fatal("second same-session run started before the first completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	<-secondStarted
	for range 3 {
		if submitErr := <-results; submitErr != nil {
			t.Fatalf("Submit() error = %v", submitErr)
		}
	}
}

func TestCoordinatorCloseRejectsPendingAndFutureSubmissions(t *testing.T) {
	var executions atomic.Int32
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: time.Second,
	}, func(_ context.Context, _ RunSubmission, _ agent.EventSink) (*assistantdomain.RunResult, error) {
		executions.Add(1)
		return &assistantdomain.RunResult{}, nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	pendingResult := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmission("run-close", "message-1", 1))
		pendingResult <- submitErr
	}()
	time.Sleep(20 * time.Millisecond)
	if err := coordinator.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if submitErr := <-pendingResult; !errors.Is(submitErr, errCoordinatorClosed) {
		t.Fatalf("pending Submit() error = %v, want coordinator closed", submitErr)
	}
	if _, submitErr := coordinator.Submit(context.Background(), testSubmission("run-future", "message-2", 2)); !errors.Is(submitErr, errCoordinatorClosed) {
		t.Fatalf("future Submit() error = %v, want coordinator closed", submitErr)
	}
	if executions.Load() != 0 {
		t.Fatalf("executions = %d, want 0", executions.Load())
	}
}

func TestCoordinatorFansExecutionErrorOutToEveryWaiter(t *testing.T) {
	executionErr := errors.New("execution failed")
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 20 * time.Millisecond,
	}, func(context.Context, RunSubmission, agent.EventSink) (*assistantdomain.RunResult, error) {
		return nil, executionErr
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	results := make(chan error, 2)
	for _, submission := range []RunSubmission{
		testSubmission("run-error", "message-1", 1),
		testSubmission("run-error", "message-2", 2),
	} {
		submission := submission
		go func() {
			_, submitErr := coordinator.Submit(context.Background(), submission)
			results <- submitErr
		}()
	}
	for range 2 {
		if submitErr := <-results; !errors.Is(submitErr, executionErr) {
			t.Fatalf("Submit() error = %v, want execution error", submitErr)
		}
	}
}

func TestCoordinatorCancelledWaiterDoesNotCorruptDebouncedBatch(t *testing.T) {
	var executions atomic.Int32
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 30 * time.Millisecond,
	}, func(context.Context, RunSubmission, agent.EventSink) (*assistantdomain.RunResult, error) {
		executions.Add(1)
		return &assistantdomain.RunResult{RunID: "run-1"}, nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancelledResult := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(cancelledCtx, testSubmission("run-1", "cancelled", 1))
		cancelledResult <- submitErr
	}()
	time.Sleep(5 * time.Millisecond)
	successResult := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmission("run-1", "active", 2))
		successResult <- submitErr
	}()
	cancel()

	if submitErr := <-cancelledResult; !errors.Is(submitErr, context.Canceled) {
		t.Fatalf("cancelled Submit() error = %v", submitErr)
	}
	if submitErr := <-successResult; submitErr != nil {
		t.Fatalf("active Submit() error = %v", submitErr)
	}
	if executions.Load() != 1 {
		t.Fatalf("executions = %d, want 1", executions.Load())
	}
}

func TestCoordinatorCloseWaitsForInflightExecution(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	coordinator, err := NewCoordinator(Config{MaxConcurrency: 1}, func(
		context.Context,
		RunSubmission,
		agent.EventSink,
	) (*assistantdomain.RunResult, error) {
		close(started)
		<-release
		return &assistantdomain.RunResult{}, nil
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	submitDone := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), RunSubmission{})
		submitDone <- submitErr
	}()
	<-started
	closeDone := make(chan error, 1)
	go func() { closeDone <- coordinator.Close() }()
	select {
	case closeErr := <-closeDone:
		t.Fatalf("Close() returned before execution completed: %v", closeErr)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if submitErr := <-submitDone; submitErr != nil {
		t.Fatalf("Submit() error = %v", submitErr)
	}
	if closeErr := <-closeDone; closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
}

func TestCoordinatorCancelDuringPendingToActiveTransition(t *testing.T) {
	executionStarted := make(chan struct{})
	coordinator, err := NewCoordinator(Config{
		MaxConcurrency: 1,
		DebounceWindow: 10 * time.Millisecond,
	}, func(ctx context.Context, _ RunSubmission, _ agent.EventSink) (*assistantdomain.RunResult, error) {
		close(executionStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	defer coordinator.Close()

	submitDone := make(chan error, 1)
	go func() {
		_, submitErr := coordinator.Submit(context.Background(), testSubmission("run-race", "message", 1))
		submitDone <- submitErr
	}()
	stopCancelling := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopCancelling:
				return
			default:
				_ = coordinator.Cancel(context.Background(), 1, 2, "session-1", "run-race")
			}
		}
	}()
	<-executionStarted
	submitErr := <-submitDone
	close(stopCancelling)
	if !errors.Is(submitErr, context.Canceled) {
		t.Fatalf("Submit() error = %v, want context.Canceled", submitErr)
	}
}

func testSubmission(runID, messageID string, sequence uint64) RunSubmission {
	return RunSubmission{
		Request: &assistantdomain.RunRequest{
			RunID:  runID,
			TaskID: "task-1",
			Input: assistantdomain.InputContext{
				Messages: []assistantdomain.InputMessage{{Role: "user", Content: messageID}},
			},
		},
		EventContext: RunEventContext{
			OrgID:             1,
			WorkerID:          2,
			SessionID:         "session-1",
			RunID:             runID,
			ReplyToMessageIDs: []string{messageID},
		},
		DeliverySeqs: []uint64{sequence},
	}
}
