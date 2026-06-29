// Package run provides the Worker-side RunCoordinator.
//
// Coordinator manages Worker-local scheduling: concurrent slot limits,
// per-session serialization, debounce merging, cancellation, and active run tracking.
//
// Coordinator MUST NOT depend on workspace, model, engine, artifact, event types, or NATS subjects.
package run

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/pkg/utils"
)

// RunSubmission is a submitted run request with event context and delivery sequences.
type RunSubmission struct {
	Request      *assistantdomain.RunRequest
	EventContext RunEventContext
	DeliverySeqs []uint64

	waiterIDs []uint64
}

// RunEventContext carries the routing/tracing context for one submission.
type RunEventContext struct {
	OrgID             uint
	WorkerID          uint
	SessionID         string
	TraceID           string
	RequestID         string
	TaskID            string
	RunID             string
	ParentID          string
	ReplyToMessageIDs []string
}

// ExecuteFunc is the actual execution function injected by the command adapter.
type ExecuteFunc func(
	ctx context.Context,
	submission RunSubmission,
	sink agent.EventSink,
) (*assistantdomain.RunResult, error)

// RunOutcome is the result of executing a run (possibly merged from multiple submissions).
type RunOutcome struct {
	Result       *assistantdomain.RunResult
	DeliverySeqs []uint64
}

// Coordinator manages Worker-local scheduling for agent runs.
type Coordinator struct {
	maxConcurrency int
	debounceWindow time.Duration
	slots          chan struct{}

	debouncer *utils.TrailingDebouncer[RunSubmission]

	activeRuns   map[string]*activeRun
	activeRunsMu sync.RWMutex

	executeFunc ExecuteFunc

	pending      map[uint64]chan runResult
	nextWaiterID uint64

	stateMu sync.Mutex
	closed  bool

	submissions sync.WaitGroup
}

type runResult struct {
	outcome RunOutcome
	err     error
}

var errCoordinatorClosed = errors.New("run coordinator is closed")

// activeRun tracks a running agent execution that can be cancelled.
type activeRun struct {
	runID     string
	taskID    string
	cancel    context.CancelFunc
	startedAt time.Time
}

// Config controls Coordinator behavior.
type Config struct {
	MaxConcurrency int
	DebounceWindow time.Duration
}

// NewCoordinator creates a new RunCoordinator.
func NewCoordinator(cfg Config, executeFunc ExecuteFunc) (*Coordinator, error) {
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 20
	}
	if cfg.DebounceWindow <= 0 {
		cfg.DebounceWindow = 1500 * time.Millisecond
	}
	if executeFunc == nil {
		return nil, fmt.Errorf("execute function is required")
	}

	c := &Coordinator{
		maxConcurrency: cfg.MaxConcurrency,
		debounceWindow: cfg.DebounceWindow,
		slots:          make(chan struct{}, cfg.MaxConcurrency),
		executeFunc:    executeFunc,
		activeRuns:     make(map[string]*activeRun),
		pending:        make(map[uint64]chan runResult),
	}

	// The debouncer merges submissions with the same session key.
	debouncer, err := utils.NewTrailingDebouncer(cfg.DebounceWindow, c.enqueueSubmission, nil, mergeSubmissions)
	if err != nil {
		return nil, err
	}
	c.debouncer = debouncer
	return c, nil
}

// Submit submits a run request. For session-keyed submissions, it goes through
// debounce merging; the caller blocks until the consolidated batch completes.
func (c *Coordinator) Submit(ctx context.Context, submission RunSubmission) (RunOutcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.beginSubmission(); err != nil {
		return RunOutcome{}, err
	}
	defer c.submissions.Done()

	key := sessionKey(submission)
	if key == "" {
		result, err := c.execute(ctx, submission)
		return RunOutcome{Result: result, DeliverySeqs: submission.DeliverySeqs}, err
	}

	return c.scheduleAndWait(ctx, key, submission)
}

func (c *Coordinator) beginSubmission() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.closed {
		return errCoordinatorClosed
	}
	c.submissions.Add(1)
	return nil
}

// scheduleAndWait registers a waiter for the session key, calls the debouncer,
// and blocks until the consolidated batch has been executed.
func (c *Coordinator) scheduleAndWait(ctx context.Context, key string, submission RunSubmission) (RunOutcome, error) {
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		return RunOutcome{}, errCoordinatorClosed
	}
	c.nextWaiterID++
	waiterID := c.nextWaiterID
	ch := make(chan runResult, 1)
	c.pending[waiterID] = ch
	submission.waiterIDs = append(submission.waiterIDs, waiterID)
	c.debouncer.Call(ctx, key, submission)
	c.stateMu.Unlock()

	select {
	case <-ctx.Done():
		c.removeWaiter(waiterID)
		return RunOutcome{}, ctx.Err()
	case rr := <-ch:
		return rr.outcome, rr.err
	}
}

// enqueueSubmission is the debouncer handler for merged submissions.
func (c *Coordinator) enqueueSubmission(ctx context.Context, submission RunSubmission) error {
	result, err := c.execute(ctx, submission)
	outcome := RunOutcome{
		Result:       result,
		DeliverySeqs: submission.DeliverySeqs,
	}
	c.notifyWaiters(submission.waiterIDs, runResult{outcome: outcome, err: err})
	return err
}

func (c *Coordinator) execute(ctx context.Context, submission RunSubmission) (*assistantdomain.RunResult, error) {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	key := sessionKey(submission)
	if key != "" {
		c.RegisterRun(key, submission.EventContext.RunID, requestTaskID(submission), cancel)
		defer c.UnregisterRun(key)
	}
	return c.executeFunc(runCtx, submission, nil)
}

func requestTaskID(submission RunSubmission) string {
	if submission.Request != nil && strings.TrimSpace(submission.Request.TaskID) != "" {
		return submission.Request.TaskID
	}
	return submission.EventContext.TaskID
}

func (c *Coordinator) removeWaiter(waiterID uint64) {
	c.stateMu.Lock()
	delete(c.pending, waiterID)
	c.stateMu.Unlock()
}

func (c *Coordinator) notifyWaiters(waiterIDs []uint64, result runResult) {
	waiters := make([]chan runResult, 0, len(waiterIDs))
	c.stateMu.Lock()
	for _, waiterID := range waiterIDs {
		if ch, ok := c.pending[waiterID]; ok {
			delete(c.pending, waiterID)
			waiters = append(waiters, ch)
		}
	}
	c.stateMu.Unlock()
	for _, ch := range waiters {
		ch <- result
	}
}

// Cancel cancels an active run for the given session and optional runID.
func (c *Coordinator) Cancel(ctx context.Context, orgID, workerID uint, sessionID, runID string) error {
	key := fmt.Sprintf("%d:%d:%s", orgID, workerID, sessionID)

	c.activeRunsMu.RLock()
	ar, ok := c.activeRuns[key]
	c.activeRunsMu.RUnlock()

	if !ok {
		return nil // no active run for this session
	}

	if runID != "" && ar.runID != runID {
		return nil // run ID mismatch
	}

	ar.cancel()
	return nil
}

// RegisterRun records an active run for cancellation tracking.
func (c *Coordinator) RegisterRun(sessionKey, runID, taskID string, cancel context.CancelFunc) {
	if sessionKey == "" {
		return
	}
	c.activeRunsMu.Lock()
	if c.activeRuns == nil {
		c.activeRuns = make(map[string]*activeRun)
	}
	c.activeRuns[sessionKey] = &activeRun{
		runID:     runID,
		taskID:    taskID,
		cancel:    cancel,
		startedAt: time.Now(),
	}
	c.activeRunsMu.Unlock()
}

// UnregisterRun removes a previously registered active run.
func (c *Coordinator) UnregisterRun(sessionKey string) {
	if sessionKey == "" {
		return
	}
	c.activeRunsMu.Lock()
	delete(c.activeRuns, sessionKey)
	c.activeRunsMu.Unlock()
}

// Close shuts down the coordinator gracefully, waiting for all in-flight runs.
func (c *Coordinator) Close() error {
	if c == nil {
		return nil
	}
	c.stateMu.Lock()
	if c.closed {
		c.stateMu.Unlock()
		c.submissions.Wait()
		return nil
	}
	c.closed = true
	c.debouncer.Close()
	waiters := make([]chan runResult, 0, len(c.pending))
	for waiterID, ch := range c.pending {
		delete(c.pending, waiterID)
		waiters = append(waiters, ch)
	}
	c.stateMu.Unlock()

	for _, ch := range waiters {
		ch <- runResult{err: errCoordinatorClosed}
	}
	c.submissions.Wait()
	return nil
}

// sessionKey builds a unique key for per-session serialization.
func sessionKey(submission RunSubmission) string {
	ec := submission.EventContext
	if ec.OrgID == 0 || ec.WorkerID == 0 || strings.TrimSpace(ec.SessionID) == "" {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", ec.OrgID, ec.WorkerID, strings.TrimSpace(ec.SessionID))
}

// mergeSubmissions merges two submissions for the same session.
func mergeSubmissions(existing RunSubmission, incoming RunSubmission) RunSubmission {
	merged := existing
	merged.DeliverySeqs = appendUniqueUint64(nil, existing.DeliverySeqs...)
	merged.DeliverySeqs = appendUniqueUint64(merged.DeliverySeqs, incoming.DeliverySeqs...)
	merged.waiterIDs = append(append([]uint64(nil), existing.waiterIDs...), incoming.waiterIDs...)
	merged.EventContext.ReplyToMessageIDs = appendUniqueString(
		append([]string(nil), existing.EventContext.ReplyToMessageIDs...),
		incoming.EventContext.ReplyToMessageIDs...,
	)

	// Merge input messages.
	if incoming.Request != nil {
		existingReq := assistantdomain.CloneRequest(merged.Request)
		if existingReq == nil {
			existingReq = &assistantdomain.RunRequest{}
		}
		incomingReq := incoming.Request
		existingReq.Input.Messages = append(existingReq.Input.Messages, incomingReq.Input.Messages...)
		existingReq.Input.Attachments = append(existingReq.Input.Attachments, incomingReq.Input.Attachments...)
		merged.Request = existingReq
	}

	return merged
}

func appendUniqueUint64(dst []uint64, values ...uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}

func appendUniqueString(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}
