// Package run provides the cmd.run lane handler for worker agent run commands.
//
// Handler receives run commands from the command.Dispatcher, validates them,
// and feeds them into the debounce/execution pipeline.
package run

import (
	"context"
	"errors"
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
	runtimeevents "github.com/insmtx/Leros/backend/internal/runtime/events"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/pkg/messaging"
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

// Config controls a worker run handler.
type Config struct {
	OrgID          uint
	WorkerID       uint
	Env            string
	DebounceWindow time.Duration
	MaxConcurrency int    // concurrent worker pool size, default 20
	SeqTrackerPath string // path to SQLite seq tracker database
}

// runTask is the internal expanded task representation.
// It is decoded from messaging.WorkerCommand in HandleRunCommand and passed
// through the debounce/pool/execute pipeline, avoiding dependency on the legacy
// protocol package.
type runTask struct {
	ID        string
	CreatedAt time.Time
	Trace     messaging.TraceContext
	Route     messaging.RouteContext
	Metadata  map[string]any

	// From RunCommandPayload (flattened)
	TaskType  messaging.TaskType
	Actor     messaging.ActorContext
	Execution messaging.ExecutionTarget
	Workspace messaging.WorkspaceOptions
	Input     messaging.TaskInput
	Model     messaging.ModelOptions
	Runtime   messaging.RuntimeOptions
	Policy    messaging.TaskPolicy
}

// Handler receives run commands and executes them via an agent.Runner.
type Handler struct {
	cfg        Config
	publisher  ResultPublisher
	runner     agent.Runner
	debouncer  *utils.TrailingDebouncer[runTask]
	pool       *workerpool.Pool
	seqTracker seqtracker.SeqTracker
	sem        chan struct{}
	pending    map[string][]chan struct{}
	pendingMu  sync.Mutex
	giteaCfg   *config.GiteaConfig

	activeRuns   map[string]*activeRun
	activeRunsMu sync.RWMutex
}

// activeRun tracks a running agent execution that can be cancelled.
type activeRun struct {
	runID     string
	taskID    string
	cancel    context.CancelFunc
	startedAt time.Time
}

// New creates a worker run handler.
func New(cfg Config, publisher ResultPublisher, runner agent.Runner, giteaCfg *config.GiteaConfig) (*Handler, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("worker org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker worker_id is required")
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

	h := &Handler{
		cfg:        cfg,
		publisher:  publisher,
		runner:     runner,
		pool:       workerpool.New(maxConcurrency),
		seqTracker: tracker,
		sem:        make(chan struct{}, maxConcurrency*2),
		pending:    make(map[string][]chan struct{}),
		activeRuns: make(map[string]*activeRun),
		giteaCfg:   giteaCfg,
	}

	// Debouncer handler changed from runTask to enqueueTask.
	// The debouncer merges rapid messages in the same session; after the quiet window,
	// enqueueTask submits the consolidated batch to the worker pool.
	debouncer, err := utils.NewTrailingDebouncer(window, h.enqueueTask, func(ctx context.Context, err error) {
		logs.ErrorContextf(ctx, "Failed to enqueue worker task: %v", err)
	}, mergeWorkerTaskMessages)
	if err != nil {
		return nil, err
	}
	h.debouncer = debouncer
	return h, nil
}

// RunSubject returns the NATS subject for this handler's cmd.run lane.
func (h *Handler) RunSubject() string {
	topic, err := messaging.WorkerCommandSubject(h.cfg.OrgID, h.cfg.WorkerID, messaging.LaneRun)
	if err != nil {
		logs.Errorf("Failed to get worker task topic for org_id=%d worker_id=%d: %v", h.cfg.OrgID, h.cfg.WorkerID, err)
	}
	return topic
}

// ---- debounce + pool + session scheduling ----

// schedule dispatches the task. For session-keyed tasks, it goes through the debouncer
// and the caller blocks until execution completes. For non-session tasks, it submits
// directly to the pool and blocks there.
func (h *Handler) schedule(ctx context.Context, task runTask) {
	key := sessionTaskKey(task)
	if key == "" {
		// No session — submit directly to pool (blocks until worker available).
		h.pool.Submit(func(execCtx context.Context) error {
			return h.executeWithTracker(execCtx, task)
		})
		return
	}

	// Has session — debounce + wait for execution.
	h.scheduleAndWait(ctx, key, task)
}

// scheduleAndWait registers a waiter for the session key, calls the debouncer, and blocks
// until the consolidated batch has been executed. Only the first message per batch waits;
// subsequent messages within the same debounce window merge and return immediately.
func (h *Handler) scheduleAndWait(ctx context.Context, key string, task runTask) {
	h.pendingMu.Lock()
	isFirst := len(h.pending[key]) == 0
	var done chan struct{}
	if isFirst {
		done = make(chan struct{})
		h.pending[key] = append(h.pending[key], done)
	}
	h.pendingMu.Unlock()

	h.debouncer.Call(ctx, key, task)

	if isFirst {
		<-done // local ref — no race when enqueueTask deletes pending[key].
	}
	// Non-first: return immediately, sem released by caller.
}

// enqueueTask is the debouncer handler. It submits the consolidated batch to the pool
// and notifies only the waiters that were registered before this batch started executing.
func (h *Handler) enqueueTask(ctx context.Context, task runTask) error {
	key := sessionTaskKey(task)

	h.pendingMu.Lock()
	waiters := h.pending[key]
	delete(h.pending, key)
	h.pendingMu.Unlock()

	// pool.Submit blocks until a worker is available (backpressure).
	h.pool.Submit(func(execCtx context.Context) error {
		defer func() {
			for _, ch := range waiters {
				close(ch)
			}
		}()
		return h.executeWithTracker(execCtx, task)
	})
	return nil
}

// executeWithTracker updates seq tracker status around the actual task execution.
func (h *Handler) executeWithTracker(ctx context.Context, task runTask) error {
	seqs := extractSeqs(task)
	topic := h.RunSubject()

	for _, s := range seqs {
		if h.seqTracker != nil {
			_ = h.seqTracker.MarkProcessing(ctx, topic, s)
		}
	}

	err := h.runTask(ctx, task)

	for _, s := range seqs {
		if h.seqTracker != nil {
			if err != nil {
				_ = h.seqTracker.MarkFailed(ctx, topic, s, err.Error())
			} else {
				_ = h.seqTracker.MarkCompleted(ctx, topic, s)
			}
		}
	}
	return err
}

// runTask executes the agent run for a consolidated task.
func (h *Handler) runTask(ctx context.Context, task runTask) error {
	req := RequestFromWorkerTask(task)
	req.EventSink = NewMQStreamSink(h.publisher, task)

	plan, err := h.prepareWorkspace(ctx, task, req)
	if err != nil {
		h.emitRunFailed(ctx, req, err)
		return err
	}

	h.ingestAttachments(ctx, req, plan)

	logs.InfoContextf(ctx,
		"Starting worker task run: task_id=%s run_id=%s runtime=%s assistant_id=%s",
		req.TaskID,
		req.RunID,
		req.Runtime.Kind,
		req.Assistant.ID,
	)

	runCtx, cancel := context.WithCancel(ctx)
	key := sessionTaskKey(task)
	h.registerRun(key, req.RunID, req.TaskID, cancel)
	defer h.unregisterRun(key)

	result, err := h.runner.Run(runCtx, req)
	// If runCtx was cancelled, ensure the returned error is context.Canceled,
	// so downstream lifecycle journal can correctly identify it as cancelled rather than failed.
	if err != nil && runCtx.Err() == context.Canceled {
		err = context.Canceled
	}
	if err != nil {
		// result == nil 表示错误发生在 PersistStep 之前（如 panic、基础设施故障），
		// 需要在这里发送终端事件作为 fallback。
		// result != nil 表示 PersistStep 已统一发送终端事件，无需重复发送。
		if result == nil {
			if isRunCancelled(result, err) {
				logs.InfoContextf(ctx, "Worker task cancelled: task_id=%s run_id=%s", req.TaskID, req.RunID)
			} else {
				h.emitRunFailed(ctx, req, err)
			}
		}
		if isRunCancelled(result, err) {
			return nil
		}
		return err
	}

	if result != nil {
		logs.InfoContextf(ctx, "Worker task completed: task_id=%s run_id=%s status=%s", req.TaskID, result.RunID, result.Status)
	}
	return nil
}

func isRunCancelled(result *agent.RunResult, err error) bool {
	if result != nil && result.Status == agent.RunStatusCancelled {
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (h *Handler) emitRunFailed(ctx context.Context, req *agent.RequestContext, runErr error) {
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

func (h *Handler) prepareWorkspace(ctx context.Context, task runTask, req *agent.RequestContext) (*agentworkspace.TaskWorkspace, error) {
	projectID := strings.TrimSpace(task.Workspace.ProjectID)
	if projectID == "" {
		workDir, err := agentworkspace.PrepareTempWorkspace()
		if err != nil {
			return nil, err
		}
		req.Runtime.WorkDir = workDir
		return nil, nil
	}
	requestID := strings.TrimSpace(task.Trace.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(task.ID)
	}

	cloneURL := ""
	if h.giteaCfg != nil {
		orgID := task.Route.OrgID
		repoName := fmt.Sprintf("%s-%d-%s", h.cfg.Env, orgID, projectID)
		endpoint := strings.TrimPrefix(strings.TrimPrefix(h.giteaCfg.Endpoint, "https://"), "http://")
		scheme := "https"
		if strings.HasPrefix(h.giteaCfg.Endpoint, "http://") {
			scheme = "http"
		}
		cloneURL = fmt.Sprintf("%s://%s:%s@%s/%s/%s.git",
			scheme,
			h.giteaCfg.Owner, h.giteaCfg.AccessToken,
			endpoint,
			h.giteaCfg.Owner, repoName)
	}

	plan, err := agentworkspace.PrepareTaskWorkspace(ctx, agentworkspace.TaskWorkspaceRequest{
		OrgID:            task.Route.OrgID,
		ProjectID:        projectID,
		TaskID:           task.Trace.TaskID,
		RequestID:        requestID,
		RequestedWorkDir: task.Runtime.WorkDir,
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
func (h *Handler) ingestAttachments(ctx context.Context, req *agent.RequestContext, plan *agentworkspace.TaskWorkspace) {
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
		logs.ErrorContextf(ctx, "git add uploads/: %v: %s", err, strings.TrimSpace(string(output)))
		return
	}
	msg := fmt.Sprintf("task: %d user attachment(s)", count)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", msg)
	commitCmd.Dir = repoDir
	commitCmd.Env = identity.GitAuthorEnv()
	if output, err := commitCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git commit attachments: %v: %s", err, strings.TrimSpace(string(output)))
		return
	}
	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = repoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git push uploads/: %v: %s", err, strings.TrimSpace(string(output)))
		return
	}
	logs.InfoContextf(ctx, "git push uploads/ completed: count=%d", count)
}

// Close shuts down the consumer gracefully, waiting for all in-flight tasks.
func (h *Handler) Close() error {
	h.pool.Close()
	if h.seqTracker != nil {
		return h.seqTracker.Close()
	}
	return nil
}

// registerRun records an active agent run that can be cancelled.
func (h *Handler) registerRun(sessionKey, runID, taskID string, cancel context.CancelFunc) {
	if sessionKey == "" {
		return
	}
	h.activeRunsMu.Lock()
	if h.activeRuns == nil {
		h.activeRuns = make(map[string]*activeRun)
	}
	h.activeRuns[sessionKey] = &activeRun{
		runID:     runID,
		taskID:    taskID,
		cancel:    cancel,
		startedAt: time.Now(),
	}
	h.activeRunsMu.Unlock()
}

// unregisterRun removes a previously registered active run.
func (h *Handler) unregisterRun(sessionKey string) {
	if sessionKey == "" {
		return
	}
	h.activeRunsMu.Lock()
	delete(h.activeRuns, sessionKey)
	h.activeRunsMu.Unlock()
}

// cancelSessionRun cancels an active run for the given session, if any.
func (h *Handler) cancelSessionRun(ctx context.Context, sessionID, runID string) {
	key := fmt.Sprintf("%d:%d:%s", h.cfg.OrgID, h.cfg.WorkerID, sessionID)

	h.activeRunsMu.RLock()
	ar, ok := h.activeRuns[key]
	h.activeRunsMu.RUnlock()

	if !ok {
		logs.InfoContextf(ctx, "cancelSessionRun: no active run for session=%s", sessionID)
		return
	}

	if runID != "" && ar.runID != runID {
		logs.InfoContextf(ctx, "cancelSessionRun: run_id mismatch session=%s want=%s got=%s", sessionID, runID, ar.runID)
		return
	}

	logs.InfoContextf(ctx, "cancelSessionRun: cancelling session=%s run=%s task=%s", sessionID, ar.runID, ar.taskID)
	ar.cancel()
}

func sessionTaskKey(task runTask) string {
	if task.Route.OrgID == 0 || task.Route.WorkerID == 0 || strings.TrimSpace(task.Route.SessionID) == "" {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", task.Route.OrgID, task.Route.WorkerID, strings.TrimSpace(task.Route.SessionID))
}

func validateModelConfig(model messaging.ModelOptions) error {
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

// mergeWorkerTaskMessages merges incoming tasks and accumulates seq numbers.
func mergeWorkerTaskMessages(existing runTask, incoming runTask) runTask {
	// Merge input messages and attachments.
	if len(incoming.Input.Messages) > 0 {
		existing.Input.Messages = append(existing.Input.Messages, incoming.Input.Messages...)
	}
	if len(incoming.Input.Attachments) > 0 {
		existing.Input.Attachments = append(existing.Input.Attachments, incoming.Input.Attachments...)
	}

	// Accumulate seq numbers from both messages.
	existingSeqs := extractSeqs(existing)
	incomingSeqs := extractSeqs(incoming)
	allSeqs := append(existingSeqs, incomingSeqs...)
	setSeqs(&existing, allSeqs)

	return existing
}

// --- seq helpers ---

func storeSeq(task *runTask, seq uint64) {
	if seq == 0 {
		return
	}
	setSeqs(task, append(extractSeqs(*task), seq))
}

func extractSeqs(task runTask) []uint64 {
	if task.Metadata == nil {
		return nil
	}
	raw, ok := task.Metadata[seqsKey]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []uint64:
		return v
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

func setSeqs(task *runTask, seqs []uint64) {
	if len(seqs) == 0 {
		return
	}
	if task.Metadata == nil {
		task.Metadata = make(map[string]any)
	}
	task.Metadata[seqsKey] = seqs
}

// ---- command dispatcher handler methods ----

// HandleRunCommand handles run commands from the command dispatcher.
//
// 执行完整的消费语义：
//  1. sem acquire（背压控制）
//  2. Decode command payload
//  3. Validate route + task type + model config
//  4. Seq tracking from NATS msg metadata（crash recovery）
//  5. Store seq in Metadata for debounce merging
//  6. scheduleAndWait — session task 走 debounce merge，第一条阻塞等执行完成
//
// HandleRunCommand 在第一条 debounced batch 执行完成后才返回，保证 NATS ACK 安全。
func (h *Handler) HandleRunCommand(ctx context.Context, cmd messaging.WorkerCommand, msg *nats.Msg) error {
	payload, err := messaging.DecodeCommandPayload[messaging.RunCommandPayload](&cmd.Body)
	if err != nil {
		return fmt.Errorf("run command payload decode: %w", err)
	}

	// Decode into internal flat representation.
	task := runTask{
		ID:        cmd.ID,
		CreatedAt: cmd.CreatedAt,
		Trace:     cmd.Trace,
		Route:     cmd.Route,
		Metadata:  cmd.Metadata,
		TaskType:  payload.TaskType,
		Actor:     payload.Actor,
		Execution: payload.Execution,
		Workspace: payload.Workspace,
		Input:     payload.Input,
		Model:     payload.Model,
		Runtime:   payload.Runtime,
		Policy:    payload.Policy,
	}

	// Acquire semaphore first — if at capacity, block here immediately (backpressure).
	h.sem <- struct{}{}

	// Validate route.
	if err := h.validateRouteTask(task); err != nil {
		<-h.sem
		return err
	}

	// Validate task type.
	if task.TaskType != messaging.TaskTypeAgentRun {
		<-h.sem
		return fmt.Errorf("unsupported task type %q", task.TaskType)
	}

	// Validate model config.
	if err := validateModelConfig(task.Model); err != nil {
		<-h.sem
		return err
	}

	// Track seq for crash recovery from NATS metadata.
	var seq uint64
	if meta, err := msg.Metadata(); err == nil {
		seq = meta.Sequence.Stream
	}
	topic := h.RunSubject()
	if h.seqTracker != nil {
		// Dedup: skip messages already in terminal state during recovery replay.
		if isTerminal, err := h.seqTracker.IsTerminal(ctx, topic, seq); err == nil && isTerminal {
			logs.InfoContextf(ctx, "Skipping terminal run command: topic=%s seq=%d", topic, seq)
			<-h.sem
			return nil
		}
		_ = h.seqTracker.TrackReceived(ctx, topic, seq,
			task.Route.SessionID, task.ID, task.Trace.TaskID, task.Trace.RunID)
	}

	// Store seq in Metadata so debounce merging accumulates all seqs.
	storeSeq(&task, seq)

	logs.InfoContextf(ctx,
		"Received run command: msg_id=%s task_id=%s run_id=%s org_id=%d worker_id=%d session_id=%s task_type=%s seq=%d",
		task.ID,
		task.Trace.TaskID,
		task.Trace.RunID,
		task.Route.OrgID,
		task.Route.WorkerID,
		task.Route.SessionID,
		task.TaskType,
		seq,
	)

	// Feed into scheduler — for session tasks goes through debounce + scheduleAndWait
	// which blocks until the consolidated batch completes (first message waits).
	// For non-session tasks, submit directly to pool.
	h.schedule(ctx, task)

	<-h.sem
	return nil
}

// HandleControlCommand handles control commands (cancel) from the command dispatcher.
func (h *Handler) HandleControlCommand(ctx context.Context, cmd messaging.WorkerCommand) error {
	switch cmd.Body.CommandType {
	case messaging.CommandTypeCancel:
		payload, err := messaging.DecodeCommandPayload[messaging.CancelRunCommandPayload](&cmd.Body)
		if err != nil {
			logs.WarnContextf(ctx, "Failed to decode cancel payload: %v", err)
			return err
		}
		h.cancelSessionRun(ctx, cmd.Route.SessionID, payload.RunID)
	default:
		logs.WarnContextf(ctx, "unknown control command type: %s", cmd.Body.CommandType)
	}
	return nil
}

func (h *Handler) validateRouteTask(task runTask) error {
	if task.Route.OrgID != 0 && task.Route.OrgID != h.cfg.OrgID {
		return fmt.Errorf("task org_id %d does not match worker org_id %d", task.Route.OrgID, h.cfg.OrgID)
	}
	if task.Route.WorkerID != 0 && task.Route.WorkerID != h.cfg.WorkerID {
		return fmt.Errorf("task worker_id %d does not match worker_id %d", task.Route.WorkerID, h.cfg.WorkerID)
	}
	return nil
}
