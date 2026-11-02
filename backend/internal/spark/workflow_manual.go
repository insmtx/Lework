package spark

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/insmtx/Leros/backend/types"
)

type workflowActorKey struct{}

// WithWorkflowActor attaches the authenticated actor without accepting caller JSON identity.
func WithWorkflowActor(ctx context.Context, actor uint) context.Context {
	return context.WithValue(ctx, workflowActorKey{}, actor)
}

// Status applies reservation capabilities only after raw observation, so the owner cannot block itself.
func (s *Service) Status(ctx context.Context) (types.SparkStatus, error) {
	status, err := s.rawStatus(ctx)
	if err != nil {
		return status, err
	}
	blocked := s.cfg.ModelWorkflow && s.workflow == nil
	if s.workflow != nil {
		r, e := s.workflow.store.Current(ctx)
		blocked = e != nil || r != nil
	}
	if blocked {
		status.CanStartModel = false
		status.CanStopModel = false
		if m := status.ModelControl; m != nil {
			m.CanStart = false
			m.CanStop = false
		}
		if g := status.MaintenanceControl; g != nil {
			g.CanPause = false
			g.CanResume = false
		}
	}
	return status, nil
}
func (c *Coordinator) reserveManual(ctx context.Context, kind, action, boot, owner string, modelRevision, gateRevision uint64) (types.SparkWorkflowOperation, error) {
	status, err := c.service.rawStatus(ctx)
	if err != nil {
		return types.SparkWorkflowOperation{}, err
	}
	m, g := status.ModelControl, status.MaintenanceControl
	if m == nil || g == nil || !g.Confirmed || status.Snapshot.BootID != boot || g.Revision != gateRevision || (kind == "manual_model" && m.Revision != modelRevision) {
		return types.SparkWorkflowOperation{}, types.ErrSparkWorkflowConflict
	}
	actor, _ := ctx.Value(workflowActorKey{}).(uint)
	now := time.Now().Unix()
	r := types.SparkWorkflowOperation{HostID: c.service.cfg.HostID, OrgID: c.service.cfg.OrgID, OperationID: uuid.NewString(), EffectOperationID: owner, Actor: actor, BindingDigest: c.binding, RunnerID: c.runner, Kind: kind, Action: action, BootID: boot, ModelID: m.ModelID, ExpectedModelRevision: m.Revision, ExpectedMaintenanceRevision: g.Revision, InitialActive: g.Active, InitialOwner: g.OperationID, PauseOwner: owner, PauseRevision: g.Revision, Phase: types.SparkWorkflowAccepted, Version: 1, CreatedAt: now, UpdatedAt: now, Deadline: now + 1200, IdleDeadline: now + 600}
	r.Fingerprint = digest(r)
	r, _, err = c.store.Begin(ctx, r)
	return r, err
}

// ChangeModel reserves the same host slot used by workflows before any legacy mutation.
func (s *Service) ChangeModel(ctx context.Context, request types.SparkModelRequest) (types.SparkModelControl, error) {
	if !s.cfg.ModelWorkflow {
		return s.changeModel(ctx, request)
	}
	if s.workflow == nil {
		return types.SparkModelControl{}, ErrUnavailable
	}
	if !ValidModelRequest(request) {
		return types.SparkModelControl{}, ErrModelConflict
	}
	c := s.workflow
	c.mu.Lock()
	defer c.mu.Unlock()
	r, err := c.reserveManual(ctx, "manual_model", request.Action, request.BootID, request.OperationID, request.ExpectedRevision, request.MaintenanceRevision)
	if err != nil {
		return types.SparkModelControl{}, err
	}
	if !c.phase(ctx, &r, types.SparkWorkflowModelIntent) || ctx.Err() != nil {
		return types.SparkModelControl{}, ErrUnavailable
	}
	result, err := s.changeModelChecked(ctx, request, r.ModelID)
	if err != nil {
		c.attention(ctx, &r, "outcome_unknown")
		return types.SparkModelControl{}, err
	}
	n := r
	n.Phase = types.SparkWorkflowWaitingModel
	n.ReceiptRevision = result.Revision
	if !c.transition(ctx, &r, n, false) {
		return types.SparkModelControl{}, ErrUnavailable
	}
	return result, nil
}

// ChangeMaintenance uses a fresh reservation ID even when pause and resume share their gate owner ID.
func (s *Service) ChangeMaintenance(ctx context.Context, request types.SparkMaintenanceRequest) (types.SparkMaintenanceReceipt, error) {
	if !s.cfg.ModelWorkflow {
		return s.changeMaintenance(ctx, request)
	}
	if s.workflow == nil {
		return types.SparkMaintenanceReceipt{}, ErrUnavailable
	}
	if !ValidMaintenanceRequest(request) {
		return types.SparkMaintenanceReceipt{}, ErrMaintenanceConflict
	}
	action := "resume"
	phase := types.SparkWorkflowResumeIntent
	if *request.Active {
		action = "pause"
		phase = types.SparkWorkflowPauseIntent
	}
	c := s.workflow
	c.mu.Lock()
	defer c.mu.Unlock()
	r, err := c.reserveManual(ctx, "manual_maintenance", action, request.BootID, request.OperationID, 0, request.ExpectedRevision)
	if err != nil {
		return types.SparkMaintenanceReceipt{}, err
	}
	if !c.phase(ctx, &r, phase) || ctx.Err() != nil {
		return types.SparkMaintenanceReceipt{}, ErrUnavailable
	}
	result, err := s.changeMaintenance(ctx, request)
	if err != nil {
		c.attention(ctx, &r, "outcome_unknown")
		return types.SparkMaintenanceReceipt{}, err
	}
	n := r
	n.Phase = types.SparkWorkflowWaitingResume
	n.ReceiptRevision = result.Revision
	if !c.transition(ctx, &r, n, false) {
		return types.SparkMaintenanceReceipt{}, ErrUnavailable
	}
	return result, nil
}
func (c *Coordinator) observeManual(ctx context.Context, r *types.SparkWorkflowOperation, status types.SparkStatus) {
	m, g := status.ModelControl, status.MaintenanceControl
	if r.Kind == "manual_model" {
		if !g.Active || g.Revision != r.ExpectedMaintenanceRevision || g.OperationID != r.InitialOwner || !modelReceipt(r, m) {
			c.attention(ctx, r, "revision_changed")
			return
		}
		if m.State == "error" || m.State == "unknown" {
			c.attention(ctx, r, "model_unconfirmed")
			return
		}
		if modelDone(r, m) {
			c.phase(ctx, r, types.SparkWorkflowCompletedPaused)
		}
		return
	}
	if g.Revision != r.ExpectedMaintenanceRevision+1 || g.OperationID != r.EffectOperationID || g.Active != (r.Action == "pause") {
		c.attention(ctx, r, "revision_changed")
		return
	}
	phase := types.SparkWorkflowCompleted
	if g.Active {
		phase = types.SparkWorkflowCompletedPaused
	}
	c.phase(ctx, r, phase)
}
