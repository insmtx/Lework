// Package taskconsumer consumes worker task messages and executes agent runs.
package taskconsumer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/agent"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	runtimeevents "github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/pkg/seqtracker"
	"github.com/insmtx/Leros/backend/pkg/utils"
	"github.com/insmtx/Leros/backend/pkg/workerpool"
	"github.com/nats-io/nats.go"
	"github.com/ygpkg/yg-go/logs"
)

const (
	defaultDebounceWindow = 1500 * time.Millisecond
	defaultMaxConcurrency = 20
	// seqsKey is the Metadata key used to carry merged sequence numbers through the debouncer.
	seqsKey = "_seqs"
)

// Config controls one standalone worker task consumer.
type Config struct {
	OrgID          uint
	WorkerID       uint
	Env            string
	DebounceWindow time.Duration
	MaxConcurrency int    // concurrent worker pool size, default 20
	SeqTrackerPath string // path to SQLite seq tracker database
}

// Consumer subscribes to one worker task topic and dispatches tasks to an agent runtime.
type Consumer struct {
	cfg        Config
	subscriber eventbus.Subscriber
	publisher  ResultPublisher
	runner     agent.Runner
	debouncer  *utils.TrailingDebouncer[protocol.WorkerTaskMessage]
	pool       *workerpool.Pool
	seqTracker seqtracker.SeqTracker
	sem        chan struct{}
	pending    map[string][]chan struct{}
	pendingMu  sync.Mutex
	giteaCfg   *config.GiteaConfig
}

// New creates a worker task consumer.
func New(cfg Config, subscriber eventbus.Subscriber, publisher ResultPublisher, runner agent.Runner, giteaCfg *config.GiteaConfig) (*Consumer, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("worker org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker worker_id is required")
	}
	if subscriber == nil {
		return nil, fmt.Errorf("subscriber is required")
	}
	if publisher == nil {
		return nil, fmt.Errorf("publisher is required")
	}
	if runner == nil {
		return nil, fmt.Errorf("agent runner is required")
	}

	window := cfg.DebounceWindow
	if window <= 0 {
		window = defaultDebounceWindow
	}
	maxConcurrency := cfg.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = defaultMaxConcurrency
	}

	// Init seq tracker if path provided.
	var tracker seqtracker.SeqTracker
	if strings.TrimSpace(cfg.SeqTrackerPath) != "" {
		var err error
		tracker, err = seqtracker.NewSQLiteTracker(cfg.SeqTrackerPath)
		if err != nil {
			return nil, fmt.Errorf("create seq tracker: %w", err)
		}
	}

	consumer := &Consumer{
		cfg:        cfg,
		subscriber: subscriber,
		publisher:  publisher,
		runner:     runner,
		pool:       workerpool.New(maxConcurrency),
		seqTracker: tracker,
		sem:        make(chan struct{}, maxConcurrency*2),
		pending:    make(map[string][]chan struct{}),
		giteaCfg:   giteaCfg,
	}

	// Debouncer handler changed from runTask to enqueueTask.
	// The debouncer merges rapid messages in the same session; after the quiet window,
	// enqueueTask submits the consolidated batch to the worker pool.
	debouncer, err := utils.NewTrailingDebouncer(window, consumer.enqueueTask, func(ctx context.Context, err error) {
		logs.ErrorContextf(ctx, "Failed to enqueue worker task: %v", err)
	}, mergeWorkerTaskMessages)
	if err != nil {
		return nil, err
	}
	consumer.debouncer = debouncer
	return consumer, nil
}

// TaskTopic returns the NATS subject consumed by this worker.
func (c *Consumer) TaskTopic() string {
	topic, err := dm.WorkerTaskSubject(c.cfg.OrgID, c.cfg.WorkerID)
	if err != nil {
		logs.Errorf("Failed to get worker task topic for org_id=%d worker_id=%d: %v", c.cfg.OrgID, c.cfg.WorkerID, err)
	}
	return topic
}

// Start subscribes to the worker task topic. On first start (no seq tracker or no
// history) it creates a durable consumer from the latest position. On restart with
// existing history it replays from the last terminal seq via SubscribeFrom, using
// SQLite as the authoritative recovery point. Only one subscription is active.
func (c *Consumer) Start(ctx context.Context) error {
	topic := c.TaskTopic()
	handler := func(msg *nats.Msg) {
		if err := c.handleEvent(ctx, msg); err != nil {
			logs.ErrorContextf(ctx, "Failed to handle worker task: %v", err)
		}
	}

	// Recovery path: if we have prior terminal seqs, replay from the last one.
	if c.seqTracker != nil {
		lastSeq, err := c.seqTracker.GetLastTerminalSeq(ctx, topic)
		if err != nil {
			logs.WarnContextf(ctx, "Failed to get last terminal seq for topic %s: %v", topic, err)
		}
		if lastSeq > 0 {
			logs.InfoContextf(ctx, "Starting worker task subscription (recovery): %s from seq %d", topic, lastSeq+1)
			return c.subscriber.SubscribeFrom(ctx, topic, int64(lastSeq+1), handler)
		}
	}

	logs.InfoContextf(ctx, "Starting worker task subscription: %s", topic)
	return c.subscriber.Subscribe(ctx, topic, dm.WorkerTaskConsumer(), handler)
}

// handleEvent processes an incoming NATS message. It acquires a semaphore slot first
// (blocks if at capacity → no Ack → NATS backpressure), then does synchronous validation
// and seq tracking, then spawns the blocking schedule+wait work into a background goroutine
// so the NATS callback returns immediately.
func (c *Consumer) handleEvent(ctx context.Context, msg *nats.Msg) error {
	// Acquire semaphore first — if at capacity, block here immediately.
	c.sem <- struct{}{}

	taskMsg, err := decodeWorkerTask(msg)
	if err != nil {
		<-c.sem
		return err
	}
	if err := c.validateRoute(taskMsg); err != nil {
		<-c.sem
		return err
	}
	if taskMsg.Body.TaskType != protocol.TaskTypeAgentRun {
		<-c.sem
		return fmt.Errorf("unsupported worker task type %q", taskMsg.Body.TaskType)
	}
	if err := validateModelConfig(taskMsg.Body.Model); err != nil {
		<-c.sem
		return err
	}

	// Track seq for crash recovery.
	var seq uint64
	if meta, err := msg.Metadata(); err == nil {
		seq = meta.Sequence.Stream
	}
	topic := c.TaskTopic()
	if c.seqTracker != nil {
		// Dedup: skip messages that already reached a terminal state during recovery replay.
		if isTerminal, err := c.seqTracker.IsTerminal(ctx, topic, seq); err == nil && isTerminal {
			logs.InfoContextf(ctx, "Skipping terminal message: topic=%s seq=%d", topic, seq)
			<-c.sem
			return nil
		}
		_ = c.seqTracker.TrackReceived(ctx, topic, seq,
			taskMsg.Route.SessionID, taskMsg.ID, taskMsg.Trace.TaskID, taskMsg.Trace.RunID)
	}

	// Store seq in Metadata so debounce merging accumulates all seqs.
	storeSeq(&taskMsg, seq)

	logs.InfoContextf(ctx,
		"Received worker task: msg_id=%s task_id=%s run_id=%s org_id=%d worker_id=%d session_id=%s task_type=%s seq=%d",
		taskMsg.ID,
		taskMsg.Trace.TaskID,
		taskMsg.Trace.RunID,
		taskMsg.Route.OrgID,
		taskMsg.Route.WorkerID,
		taskMsg.Route.SessionID,
		taskMsg.Body.TaskType,
		seq,
	)

	// Spawn background goroutine — it owns the semaphore slot and releases it on exit.
	go c.processEvent(ctx, taskMsg)
	return nil
}

// processEvent runs the blocking portion of event handling in a background goroutine.
func (c *Consumer) processEvent(ctx context.Context, taskMsg protocol.WorkerTaskMessage) {
	defer func() { <-c.sem }()
	c.schedule(ctx, taskMsg)
}

func validateModelConfig(model protocol.ModelOptions) error {
	if strings.TrimSpace(model.Provider) == "" {
		return fmt.Errorf("llm provider is required")
	}
	if strings.TrimSpace(model.Model) == "" {
		return fmt.Errorf("llm model is required")
	}
	if strings.TrimSpace(model.APIKey) == "" {
		return fmt.Errorf("llm api_key is required")
	}
	return nil
}

// schedule dispatches the task. For session-keyed tasks, it goes through the debouncer
// and the handler blocks until execution completes. For non-session tasks, it submits
// directly to the pool and blocks there.
func (c *Consumer) schedule(ctx context.Context, taskMsg protocol.WorkerTaskMessage) {
	key := sessionTaskKey(taskMsg)
	if key == "" {
		// No session — submit directly to pool (blocks until worker available).
		c.pool.Submit(func(execCtx context.Context) error {
			return c.executeWithTracker(execCtx, taskMsg)
		})
		return
	}

	// Has session — debounce + wait for execution.
	c.scheduleAndWait(ctx, key, taskMsg)
}

// scheduleAndWait registers a waiter for the session key, calls the debouncer, and blocks
// until the consolidated batch has been executed. Only the first message per batch waits;
// subsequent messages within the same debounce window merge and return immediately, so a
// single semaphore slot covers the entire session batch.
func (c *Consumer) scheduleAndWait(ctx context.Context, key string, taskMsg protocol.WorkerTaskMessage) {
	c.pendingMu.Lock()
	isFirst := len(c.pending[key]) == 0
	var done chan struct{}
	if isFirst {
		done = make(chan struct{})
		c.pending[key] = append(c.pending[key], done)
	}
	c.pendingMu.Unlock()

	c.debouncer.Call(ctx, key, taskMsg)

	if isFirst {
		<-done // local ref — no race when enqueueTask deletes pending[key].
	}
	// Non-first: return immediately, sem released by processEvent's defer.
}

// enqueueTask is the debouncer handler. It submits the consolidated batch to the pool
// and notifies only the waiters that were registered before this batch started executing.
// New waiters registered during execution belong to the next batch and are left alone.
func (c *Consumer) enqueueTask(ctx context.Context, taskMsg protocol.WorkerTaskMessage) error {
	key := sessionTaskKey(taskMsg)

	// Snapshot current waiters — any waiters registered after this point
	// belong to the next batch accumulated during execution.
	c.pendingMu.Lock()
	waiters := c.pending[key]
	delete(c.pending, key) // Clear so next batch starts fresh.
	c.pendingMu.Unlock()

	// pool.Submit blocks until a worker is available (backpressure).
	c.pool.Submit(func(execCtx context.Context) error {
		defer func() {
			for _, ch := range waiters {
				close(ch)
			}
		}()
		return c.executeWithTracker(execCtx, taskMsg)
	})
	return nil
}

// executeWithTracker updates seq tracker status around the actual task execution.
func (c *Consumer) executeWithTracker(ctx context.Context, taskMsg protocol.WorkerTaskMessage) error {
	seqs := extractSeqs(taskMsg)
	topic := c.TaskTopic()

	for _, s := range seqs {
		if c.seqTracker != nil {
			_ = c.seqTracker.MarkProcessing(ctx, topic, s)
		}
	}

	err := c.runTask(ctx, taskMsg)

	for _, s := range seqs {
		if c.seqTracker != nil {
			if err != nil {
				_ = c.seqTracker.MarkFailed(ctx, topic, s, err.Error())
			} else {
				_ = c.seqTracker.MarkCompleted(ctx, topic, s)
			}
		}
	}
	return err
}

// runTask executes the agent run for a consolidated task message. Existing logic preserved.
func (c *Consumer) runTask(ctx context.Context, taskMsg protocol.WorkerTaskMessage) error {
	req := RequestFromWorkerTask(taskMsg)
	req.EventSink = NewMQStreamSink(c.publisher, taskMsg)

	plan, err := c.prepareWorkspace(ctx, taskMsg, req)
	if err != nil {
		c.emitRunFailed(ctx, req, err)
		return err
	}

	c.ingestAttachments(ctx, req, plan)

	logs.InfoContextf(ctx,
		"Starting worker task run: task_id=%s run_id=%s runtime=%s assistant_id=%s",
		req.TaskID,
		req.RunID,
		req.Runtime.Kind,
		req.Assistant.ID,
	)

	result, err := c.runner.Run(ctx, req)
	if err != nil {
		c.emitRunFailed(ctx, req, err)
		return err
	}

	if result != nil {
		logs.InfoContextf(ctx, "Worker task completed: task_id=%s run_id=%s status=%s", req.TaskID, result.RunID, result.Status)
	}
	return nil
}

func (c *Consumer) emitRunFailed(ctx context.Context, req *agent.RequestContext, runErr error) {
	if req == nil || req.EventSink == nil || runErr == nil {
		return
	}

	// 确保 worker 执行失败时前端 SSE 能收到终止事件，避免会话长期停留在“生成中”。
	if err := req.EventSink.Emit(ctx, &runtimeevents.Event{
		RunID:     req.RunID,
		TraceID:   req.TraceID,
		Type:      runtimeevents.EventFailed,
		CreatedAt: time.Now().UTC(),
		Content:   runErr.Error(),
	}); err != nil {
		logs.WarnContextf(ctx, "Failed to emit worker run failure event: task_id=%s run_id=%s error=%v",
			req.TaskID, req.RunID, err)
	}
}

func (c *Consumer) prepareWorkspace(ctx context.Context, taskMsg protocol.WorkerTaskMessage, req *agent.RequestContext) (*agentworkspace.TaskWorkspace, error) {
	projectID := strings.TrimSpace(taskMsg.Body.Workspace.ProjectID)
	if projectID == "" {
		workDir, err := agentworkspace.PrepareTempWorkspace()
		if err != nil {
			return nil, err
		}
		req.Runtime.WorkDir = workDir
		return nil, nil
	}
	requestID := strings.TrimSpace(taskMsg.Trace.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(taskMsg.ID)
	}

	cloneURL := ""
	if c.giteaCfg != nil {
		orgID := taskMsg.Route.OrgID
		repoName := fmt.Sprintf("%s-%d-%s", c.cfg.Env, orgID, projectID)
		endpoint := strings.TrimPrefix(strings.TrimPrefix(c.giteaCfg.Endpoint, "https://"), "http://")
		scheme := "https"
		if strings.HasPrefix(c.giteaCfg.Endpoint, "http://") {
			scheme = "http"
		}
		cloneURL = fmt.Sprintf("%s://%s:%s@%s/%s/%s.git",
			scheme,
			c.giteaCfg.Owner, c.giteaCfg.AccessToken,
			endpoint,
			c.giteaCfg.Owner, repoName)
	}

	plan, err := agentworkspace.PrepareTaskWorkspace(ctx, agentworkspace.TaskWorkspaceRequest{
		OrgID:            taskMsg.Route.OrgID,
		ProjectID:        projectID,
		TaskID:           taskMsg.Trace.TaskID,
		RequestID:        requestID,
		RequestedWorkDir: taskMsg.Body.Runtime.WorkDir,
		CloneURL:         cloneURL,
	})
	if err != nil {
		return nil, err
	}
	req.Runtime.WorkDir = plan.EffectiveWorkDir
	req.Workspace.RepoDir = plan.RepoDir
	return plan, nil
}

// ingestAttachments downloads input attachments into the workspace repo and commits them.
// It is best-effort: download failures are logged but do not block the agent run.
// When there is no project repo (plan == nil, temp workspace), attachments are still
// downloaded to the effective work dir but not committed to git.
func (c *Consumer) ingestAttachments(ctx context.Context, req *agent.RequestContext, plan *agentworkspace.TaskWorkspace) {
	if req == nil || len(req.Input.Attachments) == 0 {
		return
	}

	var targetDir string
	var repoDir string
	if plan != nil {
		targetDir = filepath.Join(plan.RepoDir, "uploads")
		repoDir = plan.RepoDir
	} else {
		targetDir = filepath.Join(req.Runtime.WorkDir, "uploads")
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		logs.WarnContextf(ctx, "ingest attachments: create uploads dir: %v", err)
		return
	}

	var downloadedCount int
	for _, att := range req.Input.Attachments {
		if strings.TrimSpace(att.URL) == "" || strings.TrimSpace(att.Name) == "" {
			continue
		}
		if err := downloadFile(ctx, att.URL, filepath.Join(targetDir, att.Name)); err != nil {
			logs.WarnContextf(ctx, "ingest attachment %q: %v", att.Name, err)
			continue
		}
		downloadedCount++
	}

	if downloadedCount == 0 {
		return
	}

	if repoDir != "" {
		gitDir := filepath.Join(repoDir, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			commitAttachments(ctx, repoDir, downloadedCount)
		}
	}
}

// downloadFile fetches a file from url and writes it to destPath.
func downloadFile(ctx context.Context, url string, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// commitAttachments stages, commits and pushes attachment files in the workspace repo.
func commitAttachments(ctx context.Context, repoDir string, count int) {
	addCmd := exec.CommandContext(ctx, "git", "add", "uploads/")
	addCmd.Dir = repoDir
	if output, err := addCmd.CombinedOutput(); err != nil {
		logs.WarnContextf(ctx, "git add uploads/: %v: %s", err, strings.TrimSpace(string(output)))
		return
	}
	msg := fmt.Sprintf("task: %d user attachment(s)", count)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", msg)
	commitCmd.Dir = repoDir
	commitCmd.CombinedOutput()
	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = repoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git push uploads failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

// Close shuts down the consumer gracefully, waiting for all in-flight tasks.
func (c *Consumer) Close() error {
	c.pool.Close()
	if c.seqTracker != nil {
		return c.seqTracker.Close()
	}
	return nil
}

func sessionTaskKey(msg protocol.WorkerTaskMessage) string {
	if msg.Route.OrgID == 0 || msg.Route.WorkerID == 0 || strings.TrimSpace(msg.Route.SessionID) == "" {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", msg.Route.OrgID, msg.Route.WorkerID, strings.TrimSpace(msg.Route.SessionID))
}

func decodeWorkerTask(msg *nats.Msg) (protocol.WorkerTaskMessage, error) {
	var taskMsg protocol.WorkerTaskMessage
	if err := json.Unmarshal(msg.Data, &taskMsg); err != nil {
		return taskMsg, fmt.Errorf("unmarshal worker task: %w", err)
	}
	if taskMsg.Type != "" && taskMsg.Type != protocol.MessageTypeWorkerTask {
		return taskMsg, fmt.Errorf("unexpected worker task message type %q", taskMsg.Type)
	}
	return taskMsg, nil
}

func (c *Consumer) validateRoute(msg protocol.WorkerTaskMessage) error {
	if msg.Route.OrgID != 0 && msg.Route.OrgID != c.cfg.OrgID {
		return fmt.Errorf("task org_id %d does not match worker org_id %d", msg.Route.OrgID, c.cfg.OrgID)
	}
	if msg.Route.WorkerID != 0 && msg.Route.WorkerID != c.cfg.WorkerID {
		return fmt.Errorf("task worker_id %d does not match worker_id %d", msg.Route.WorkerID, c.cfg.WorkerID)
	}
	return nil
}

// mergeWorkerTaskMessages merges incoming task messages and accumulates seq numbers.
func mergeWorkerTaskMessages(existing protocol.WorkerTaskMessage, incoming protocol.WorkerTaskMessage) protocol.WorkerTaskMessage {
	// Merge input messages and attachments.
	if len(incoming.Body.Input.Messages) > 0 {
		existing.Body.Input.Messages = append(existing.Body.Input.Messages, incoming.Body.Input.Messages...)
	}
	if len(incoming.Body.Input.Attachments) > 0 {
		existing.Body.Input.Attachments = append(existing.Body.Input.Attachments, incoming.Body.Input.Attachments...)
	}

	// Accumulate seq numbers from both messages.
	existingSeqs := extractSeqs(existing)
	incomingSeqs := extractSeqs(incoming)
	allSeqs := append(existingSeqs, incomingSeqs...)
	setSeqs(&existing, allSeqs)

	return existing
}

// --- seq helpers ---

func storeSeq(msg *protocol.WorkerTaskMessage, seq uint64) {
	if seq == 0 {
		return
	}
	setSeqs(msg, append(extractSeqs(*msg), seq))
}

func extractSeqs(msg protocol.WorkerTaskMessage) []uint64 {
	if msg.Metadata == nil {
		return nil
	}
	raw, ok := msg.Metadata[seqsKey]
	if !ok {
		return nil
	}
	// The value is stored as []uint64 by storeSeq/setSeqs.
	switch v := raw.(type) {
	case []uint64:
		return v
	// During JSON round-trip (e.g. tests), []uint64 may become []interface{}.
	case []interface{}:
		seqs := make([]uint64, 0, len(v))
		for _, item := range v {
			switch n := item.(type) {
			case float64:
				seqs = append(seqs, uint64(n))
			case uint64:
				seqs = append(seqs, n)
			}
		}
		return seqs
	default:
		return nil
	}
}

func setSeqs(msg *protocol.WorkerTaskMessage, seqs []uint64) {
	if len(seqs) == 0 {
		return
	}
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}
	msg.Metadata[seqsKey] = seqs
}
