package spark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/insmtx/Leros/backend/types"
)

// WorkflowStore requires durable atomic reservation and compare-and-swap transitions.
type WorkflowStore interface {
	Begin(context.Context, types.SparkWorkflowOperation) (types.SparkWorkflowOperation, bool, error)
	Get(context.Context, string) (*types.SparkWorkflowOperation, error)
	Current(context.Context) (*types.SparkWorkflowOperation, error)
	Transition(context.Context, types.SparkWorkflowOperation, types.SparkWorkflowOperation, bool) error
}

// Coordinator continues accepted work under the process context; it never replays uncertain intents.
type Coordinator struct {
	mu      sync.Mutex
	service *Service
	store   WorkflowStore
	runner  string
	binding string
	wake    chan struct{}
}

// NewCoordinator enables workflows only after the caller initialized a durable store.
func NewCoordinator(s *Service, store WorkflowStore) (*Coordinator, error) {
	if s == nil || store == nil || !s.cfg.ModelWorkflow || !s.cfg.ModelControl || !s.cfg.MaintenanceControl {
		return nil, ErrNotConfigured
	}
	workers := append([]uint(nil), s.cfg.WorkerIDs...)
	sort.Slice(workers, func(i, j int) bool { return workers[i] < workers[j] })
	binding := struct {
		Org          uint
		Host, Socket string
		Workers      []uint
	}{s.cfg.OrgID, s.cfg.HostID, s.cfg.SocketPath, workers}
	c := &Coordinator{service: s, store: store, runner: uuid.NewString(), binding: digest(binding), wake: make(chan struct{}, 1)}
	s.workflow = c
	return c, nil
}
func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func ValidWorkflowRequest(r types.SparkWorkflowRequest) bool {
	return operationIDPattern.MatchString(r.OperationID) && shortText(r.BootID) && (r.Action == "start" || r.Action == "stop") &&
		r.ExpectedModelRevision > 0 && r.ExpectedModelRevision < 9007199254740991 && r.ExpectedMaintenanceRevision > 0 && r.ExpectedMaintenanceRevision < 9007199254740990
}
func projection(r types.SparkWorkflowOperation) types.SparkWorkflow {
	phase, code := r.Phase, r.ErrorCode
	if phase != types.SparkWorkflowCompleted && phase != types.SparkWorkflowCompletedPaused && phase != types.SparkWorkflowAttention && time.Now().Unix() >= r.Deadline {
		phase = types.SparkWorkflowAttention
		code = "deadline_exceeded"
	}
	return types.SparkWorkflow{OperationID: r.OperationID, Action: r.Action, Kind: r.Kind, Phase: phase, Version: r.Version, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Deadline: r.Deadline, ErrorCode: code, NeedsAttention: phase == types.SparkWorkflowAttention, MaintenanceRetained: phase == types.SparkWorkflowCompletedPaused}
}
func (s *Service) WorkflowCurrent(ctx context.Context) (types.SparkWorkflowControl, error) {
	out := types.SparkWorkflowControl{Enabled: s.workflow != nil}
	if s.workflow == nil {
		if s.cfg.ModelWorkflow {
			return out, ErrUnavailable
		}
		return out, nil
	}
	r, err := s.workflow.store.Current(ctx)
	if err != nil {
		return out, err
	}
	if r != nil {
		p := projection(*r)
		out.Current = &p
		return out, nil
	}
	status, err := s.rawStatus(ctx)
	if err != nil {
		return out, nil
	} // 能力未知保持 false，持久记录仍可读取。
	out.CanStart = workflowAllowed(status, "start")
	out.CanStop = workflowAllowed(status, "stop")
	return out, nil
}
func (s *Service) WorkflowGet(ctx context.Context, id string) (types.SparkWorkflow, error) {
	if s.workflow == nil {
		return types.SparkWorkflow{}, ErrNotConfigured
	}
	if !operationIDPattern.MatchString(id) {
		return types.SparkWorkflow{}, types.ErrSparkWorkflowConflict
	}
	r, err := s.workflow.store.Get(ctx, id)
	if err != nil {
		return types.SparkWorkflow{}, err
	}
	if r == nil {
		return types.SparkWorkflow{}, types.ErrSparkWorkflowConflict
	}
	return projection(*r), nil
}
func workflowAllowed(s types.SparkStatus, action string) bool {
	g, m := s.MaintenanceControl, s.ModelControl
	return s.ValidUntil >= time.Now().Unix() && g != nil && g.Confirmed && g.Revision < 9007199254740990 && m != nil && !m.Busy && m.Revision < 9007199254740991 &&
		((action == "start" && m.State == "stopped") || (action == "stop" && m.State == "ready"))
}

// BeginWorkflow persists acceptance before waking the process worker, independent of the browser lifetime.
func (s *Service) BeginWorkflow(ctx context.Context, actor uint, request types.SparkWorkflowRequest) (types.SparkWorkflow, error) {
	if s.workflow == nil {
		return types.SparkWorkflow{}, ErrNotConfigured
	}
	return s.workflow.begin(ctx, actor, request)
}
func (c *Coordinator) begin(ctx context.Context, actor uint, request types.SparkWorkflowRequest) (types.SparkWorkflow, error) {
	if !ValidWorkflowRequest(request) || actor == 0 {
		return types.SparkWorkflow{}, types.ErrSparkWorkflowConflict
	}
	fingerprint := digest(request)
	old, err := c.store.Get(ctx, request.OperationID)
	if err != nil {
		return types.SparkWorkflow{}, err
	}
	if old != nil {
		if old.Fingerprint != fingerprint || old.Kind != "workflow" {
			return types.SparkWorkflow{}, types.ErrSparkWorkflowConflict
		}
		return projection(*old), nil
	}
	status, err := c.service.rawStatus(ctx)
	if err != nil {
		return types.SparkWorkflow{}, err
	}
	if !workflowAllowed(status, request.Action) || status.Snapshot.BootID != request.BootID || status.ModelControl.Revision != request.ExpectedModelRevision || status.MaintenanceControl.Revision != request.ExpectedMaintenanceRevision {
		return types.SparkWorkflow{}, types.ErrSparkWorkflowConflict
	}
	now := time.Now().Unix()
	g := status.MaintenanceControl
	r := types.SparkWorkflowOperation{HostID: c.service.cfg.HostID, OrgID: c.service.cfg.OrgID, OperationID: request.OperationID, Actor: actor, Fingerprint: fingerprint, BindingDigest: c.binding, RunnerID: c.runner, Kind: "workflow", Action: request.Action, BootID: request.BootID, ModelID: status.ModelControl.ModelID, ExpectedModelRevision: request.ExpectedModelRevision, ExpectedMaintenanceRevision: request.ExpectedMaintenanceRevision, InitialActive: g.Active, InitialOwner: g.OperationID, PauseOwner: g.OperationID, PauseRevision: g.Revision, Phase: types.SparkWorkflowAccepted, Version: 1, CreatedAt: now, UpdatedAt: now, Deadline: now + 1200, IdleDeadline: now + 600}
	r, created, err := c.store.Begin(ctx, r)
	if err != nil {
		return types.SparkWorkflow{}, err
	}
	if created {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
	return projection(r), nil
}

// Run is owned and joined by cmd. An instance can execute only records it accepted.
func (c *Coordinator) Run(ctx context.Context) {
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		c.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-c.wake:
		}
	}
}
func (c *Coordinator) transition(ctx context.Context, r *types.SparkWorkflowOperation, next types.SparkWorkflowOperation, release bool) bool {
	next.Version = r.Version + 1
	next.UpdatedAt = time.Now().Unix()
	if c.store.Transition(ctx, *r, next, release) != nil {
		return false
	}
	*r = next
	return true
}
func (c *Coordinator) phase(ctx context.Context, r *types.SparkWorkflowOperation, p types.SparkWorkflowPhase) bool {
	n := *r
	n.Phase = p
	return c.transition(ctx, r, n, p == types.SparkWorkflowCompleted || p == types.SparkWorkflowCompletedPaused)
}
func (c *Coordinator) attention(ctx context.Context, r *types.SparkWorkflowOperation, code string) {
	n := *r
	n.Phase = types.SparkWorkflowAttention
	n.ErrorCode = code
	c.transition(ctx, r, n, false)
}
func (c *Coordinator) step(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, err := c.store.Current(ctx)
	if err != nil || r == nil || ctx.Err() != nil {
		return
	}
	if r.Phase == types.SparkWorkflowAttention || r.Phase == types.SparkWorkflowCompleted || r.Phase == types.SparkWorkflowCompletedPaused {
		return
	}
	// 不接管其他进程的未完成记录，任何 intent 都只能由其提交者当场发送一次。
	if r.RunnerID != c.runner {
		return
	}
	if time.Now().Unix() >= r.Deadline {
		c.attention(ctx, r, "deadline_exceeded")
		return
	}
	switch r.Phase {
	case types.SparkWorkflowPauseIntent, types.SparkWorkflowModelIntent, types.SparkWorkflowResumeIntent:
		c.attention(ctx, r, "outcome_unknown")
		return
	}
	if r.BindingDigest != c.binding {
		c.attention(ctx, r, "binding_changed")
		return
	}
	status, err := c.service.rawStatus(ctx)
	if err != nil {
		return
	}
	m, g := status.ModelControl, status.MaintenanceControl
	if status.Snapshot.BootID != r.BootID || m == nil || m.ModelID != r.ModelID {
		c.attention(ctx, r, "identity_changed")
		return
	}
	if g == nil || !g.Confirmed || status.ValidUntil < time.Now().Unix() {
		return
	}
	if r.Kind != "workflow" {
		c.observeManual(ctx, r, status)
		return
	}
	switch r.Phase {
	case types.SparkWorkflowAccepted:
		if g.Active != r.InitialActive || g.Revision != r.ExpectedMaintenanceRevision || g.OperationID != r.InitialOwner || m.Revision != r.ExpectedModelRevision {
			c.attention(ctx, r, "revision_changed")
			return
		}
		if r.InitialActive {
			c.phase(ctx, r, types.SparkWorkflowWaitingIdle)
			return
		}
		if !c.phase(ctx, r, types.SparkWorkflowPauseIntent) || ctx.Err() != nil {
			return
		}
		active := true
		receipt, err := c.service.changeMaintenance(ctx, types.SparkMaintenanceRequest{BootID: r.BootID, ExpectedRevision: r.ExpectedMaintenanceRevision, OperationID: r.OperationID, Active: &active})
		if err != nil {
			c.attention(ctx, r, "outcome_unknown")
			return
		}
		n := *r
		n.OwnsPause = true
		n.PauseRevision = receipt.Revision
		n.PauseOwner = receipt.OperationID
		n.ReceiptRevision = receipt.Revision
		n.Phase = types.SparkWorkflowWaitingIdle
		c.transition(ctx, r, n, false)
	case types.SparkWorkflowWaitingIdle:
		if !pausedGate(r, g) || m.Revision != r.ExpectedModelRevision {
			c.attention(ctx, r, "revision_changed")
			return
		}
		if time.Now().Unix() >= r.IdleDeadline {
			c.attention(ctx, r, "idle_timeout")
			return
		}
		if (r.Action == "start" && !m.CanStart) || (r.Action == "stop" && !m.CanStop) {
			return
		}
		if !c.phase(ctx, r, types.SparkWorkflowModelIntent) || ctx.Err() != nil {
			return
		}
		receipt, err := c.service.changeModelChecked(ctx, types.SparkModelRequest{BootID: r.BootID, ExpectedRevision: r.ExpectedModelRevision, MaintenanceRevision: r.PauseRevision, OperationID: r.OperationID, Action: r.Action}, r.ModelID)
		if err != nil {
			c.attention(ctx, r, "outcome_unknown")
			return
		}
		n := *r
		n.Phase = types.SparkWorkflowWaitingModel
		n.ReceiptRevision = receipt.Revision
		c.transition(ctx, r, n, false)
	case types.SparkWorkflowWaitingModel:
		if !pausedGate(r, g) || !modelReceipt(r, m) {
			c.attention(ctx, r, "revision_changed")
			return
		}
		if m.State == "error" || m.State == "unknown" {
			c.attention(ctx, r, "model_unconfirmed")
			return
		}
		if m.Busy {
			return
		}
		if !modelDone(r, m) {
			c.attention(ctx, r, "model_unconfirmed")
			return
		}
		if r.Action == "stop" || !r.OwnsPause {
			c.phase(ctx, r, types.SparkWorkflowCompletedPaused)
			return
		}
		if !g.CanResume {
			return
		}
		if !c.phase(ctx, r, types.SparkWorkflowResumeIntent) || ctx.Err() != nil {
			return
		}
		active := false
		receipt, err := c.service.changeMaintenanceChecked(ctx, types.SparkMaintenanceRequest{BootID: r.BootID, ExpectedRevision: r.PauseRevision, OperationID: r.PauseOwner, Active: &active}, r)
		if err != nil {
			c.attention(ctx, r, "outcome_unknown")
			return
		}
		n := *r
		n.Phase = types.SparkWorkflowWaitingResume
		n.ReceiptRevision = receipt.Revision
		c.transition(ctx, r, n, false)
	case types.SparkWorkflowWaitingResume:
		if g.Active || g.Revision != r.PauseRevision+1 || g.OperationID != r.PauseOwner || !modelReceipt(r, m) || !modelDone(r, m) {
			c.attention(ctx, r, "resume_unconfirmed")
			return
		}
		c.phase(ctx, r, types.SparkWorkflowCompleted)
	}
}
func pausedGate(r *types.SparkWorkflowOperation, g *types.SparkMaintenanceControl) bool {
	return g.Active && g.Revision == r.PauseRevision && g.OperationID == r.PauseOwner
}
func modelReceipt(r *types.SparkWorkflowOperation, m *types.SparkModelControl) bool {
	return m.BootID == r.BootID && m.ModelID == r.ModelID && m.OperationID == effectID(r) && m.Action == r.Action && m.Revision == r.ExpectedModelRevision+1
}
func modelDone(r *types.SparkWorkflowOperation, m *types.SparkModelControl) bool {
	return !m.Busy && ((r.Action == "start" && m.State == "ready") || (r.Action == "stop" && m.State == "stopped"))
}

func effectID(r *types.SparkWorkflowOperation) string {
	if r.EffectOperationID != "" {
		return r.EffectOperationID
	}
	return r.OperationID
}
