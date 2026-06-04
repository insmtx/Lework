// Package externalcli 将外部代理 CLI 适配到 Leros agent.Runner 边界。
package externalcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/internal/runtime/todo"
	"github.com/ygpkg/yg-go/logs"
)

// Runner 将外部代理 CLI 引擎适配到代理运行器边界。
type Runner struct {
	name            string
	engine          engines.Engine
	sessionStore    ProviderSessionStore
	approvalHandler engines.ApprovalHandler
}

// NewRunner 创建外部 CLI 运行器。
func NewRunner(name string, engine engines.Engine) (*Runner, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("runtime name is required")
	}
	if engine == nil {
		return nil, fmt.Errorf("runtime %q engine is nil", name)
	}
	return &Runner{
		name:         name,
		engine:       engine,
		sessionStore: DefaultProviderSessionStore(),
	}, nil
}

// SetSessionStore 替换用于外部 CLI 恢复的提供者会话存储。
func (r *Runner) SetSessionStore(store ProviderSessionStore) {
	if r == nil || store == nil {
		return
	}
	r.sessionStore = store
}

// SetApprovalHandler 设置审批处理器，用于 on-request 和 auto 模式。
func (r *Runner) SetApprovalHandler(handler engines.ApprovalHandler) {
	if r == nil {
		return
	}
	r.approvalHandler = handler
}

// Run 通过外部 CLI 引擎执行标准化请求。
func (r *Runner) Run(ctx context.Context, req *agent.RequestContext) (*agent.RunResult, error) {
	startedAt := time.Now().UTC()
	if r == nil || r.engine == nil {
		return nil, fmt.Errorf("external CLI runtime is not initialized")
	}
	if req == nil {
		return nil, fmt.Errorf("request context is required")
	}
	eventSink := sinkForRequest(req)

	workDir := strings.TrimSpace(req.Runtime.WorkDir)
	if err := r.engine.Prepare(ctx, engines.PrepareRequest{WorkDir: workDir}); err != nil {
		return r.failedResult(req, startedAt, err, failureMetadata(workDir)), err
	}

	prompt := agent.BuildUserInput(req)
	sessionPlan := r.resolveProviderSession(ctx, req, workDir)

	handle, err := r.engine.Run(ctx, engines.RunRequest{
		ExecutionID:     req.RunID,
		SessionID:       sessionPlan.ProviderSessionID,
		Resume:          sessionPlan.Resume,
		WorkDir:         workDir,
		SystemPrompt:    strings.TrimSpace(req.SystemPrompt),
		Prompt:          prompt,
		Model:           modelForRequest(req),
		ExtraEnv:        nil,
		PermissionMode:  engines.PermissionMode(req.Policy.PermissionMode),
		ApprovalHandler: r.approvalHandler,
	})
	if err != nil {
		return r.failedResult(req, startedAt, err, failureMetadata(workDir)), err
	}

	if handle != nil && handle.Process != nil {
		logs.InfoContextf(ctx, "External runtime %s started with pid %d", r.name, handle.Process.PID())
	}

	consumeResult, err := consumeEvents(ctx, eventSink, handle, req.RunID, req.TraceID, r.approvalHandler)
	if err != nil {
		r.markProviderSessionFailed(ctx, sessionPlan, err)
		return r.failedResult(req, startedAt, err, failureMetadata(workDir)), err
	}
	r.persistProviderSession(ctx, sessionPlan, consumeResult.ProviderSessionID)

	result := &agent.RunResult{
		RunID:       req.RunID,
		TraceID:     req.TraceID,
		Status:      agent.RunStatusCompleted,
		Message:     strings.TrimSpace(consumeResult.Message),
		Usage:       consumeResult.Usage,
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
		Metadata: map[string]any{
			"runtime":     r.name,
			"work_dir":    workDir,
			"resume":      sessionPlan.Resume,
			"session_id":  sessionPlan.InternalSessionID,
			"provider_id": firstNonEmptyString(sessionPlan.ProviderSessionID, consumeResult.ProviderSessionID),
		},
	}
	return result, nil
}

type consumeResult struct {
	Message           string
	ProviderSessionID string
	Usage             *events.UsagePayload
}

func consumeEvents(ctx context.Context, sink events.Sink, handle *engines.RunHandle, runID string, traceID string, approvalHandler engines.ApprovalHandler) (consumeResult, error) {
	if handle == nil || handle.Events == nil {
		return consumeResult{}, nil
	}
	if sink == nil {
		sink = events.NewNoopSink()
	}
	var result strings.Builder
	resultSeen := false
	consumed := consumeResult{}
	messageIDs := events.NewMessageIDMapper()
	todoTracker := runtimetodo.NewTracker(runtimetodo.Options{RunID: runID, TraceID: traceID, Sink: sink})
	for event := range handle.Events {
		switch event.Type {
		case events.EventStarted:
			continue
		case engines.EventProviderSessionStarted:
			if strings.TrimSpace(event.Content) != "" {
				consumed.ProviderSessionID = strings.TrimSpace(event.Content)
			}
		case events.EventResult:
			if resultPayload, err := events.DecodePayload[events.MessageResultPayload](&event); err == nil {
				if strings.TrimSpace(resultPayload.Message) != "" {
					result.Reset()
					result.WriteString(resultPayload.Message)
					resultSeen = true
				}
				if resultPayload.Usage != nil {
					consumed.Usage = resultPayload.Usage
				}
			} else if strings.TrimSpace(event.Content) != "" {
				result.Reset()
				result.WriteString(event.Content)
				resultSeen = true
			}
		case events.EventCompleted:
			consumed.Message = result.String()
			return consumed, nil
		case events.EventFailed:
			if strings.TrimSpace(event.Content) == "" {
				consumed.Message = result.String()
				return consumed, fmt.Errorf("external runtime failed")
			}
			consumed.Message = result.String()
			return consumed, fmt.Errorf("%s", event.Content)
		case events.EventMessageDelta:
			if strings.TrimSpace(event.Content) != "" {
				_ = sink.Emit(ctx, normalizeRuntimeEvent(event, messageIDs))
				if !resultSeen {
					result.WriteString(event.Content)
				}
			}
		case events.EventReasoningDelta:
			_ = sink.Emit(ctx, normalizeRuntimeEvent(event, messageIDs))
		case events.EventToolCallStarted, events.EventToolCallCompleted, events.EventToolCallFailed:
			_ = sink.Emit(ctx, normalizeRuntimeEvent(event, messageIDs))
		case events.EventTodoSnapshot:
			if items, err := events.DecodePayload[[]events.RuntimeTodoItem](&event); err == nil {
				_ = todoTracker.Snapshot(ctx, items)
			}
		case events.EventTodoUpdated:
			if items, err := events.DecodePayload[[]events.RuntimeTodoItem](&event); err == nil {
				_ = todoTracker.Update(ctx, items, true)
			}
		case events.EventApprovalRequested:
			_ = sink.Emit(ctx, normalizeRuntimeEvent(event, messageIDs))
			if handle.Responder == nil {
				logs.WarnContextf(ctx, "approval request dropped: no Responder (PermissionMode may need to be on-request/auto)")
			}
			if approvalHandler != nil && handle.Responder != nil {
				req, decErr := events.DecodePayload[events.ApprovalRequestPayload](&event)
				if decErr != nil {
					logs.WarnContextf(ctx, "decode approval request: %v", decErr)
					continue
				}
				decision, decErr := approvalHandler.RequestApproval(ctx, &engines.ApprovalRequest{
					RequestID:   req.RequestID,
					ToolCallID:  req.ToolCallID,
					ToolName:    req.ToolName,
					Arguments:   req.Arguments,
					Description: req.Description,
					Engine:      metadataString(req.Metadata, "engine"),
				})
				if decErr != nil {
					logs.WarnContextf(ctx, "approval handler error: %v", decErr)
					continue
				}
				if wErr := handle.Responder.WriteDecision(req.RequestID, decision.Action); wErr != nil {
					logs.WarnContextf(ctx, "write approval decision to stdin: %v", wErr)
				}
				_ = sink.Emit(ctx, normalizeRuntimeEvent(*events.NewApprovalResolved(events.ApprovalDecisionPayload{
					RequestID: req.RequestID,
					Action:    decision.Action,
					Reason:    decision.Reason,
				}), messageIDs))
			}
		case events.EventApprovalResolved:
			_ = sink.Emit(ctx, normalizeRuntimeEvent(event, messageIDs))
		default:
			if strings.TrimSpace(event.Content) != "" {
				if !resultSeen {
					result.WriteString(event.Content)
				}
			}
		}
	}
	consumed.Message = result.String()
	return consumed, nil
}

func normalizeRuntimeEvent(event events.Event, messageIDs *events.MessageIDMapper) *events.Event {
	switch event.Type {
	case events.EventMessageDelta:
		if len(event.Payload) > 0 {
			payload, err := events.DecodePayload[events.MessageDeltaPayload](&event)
			if err == nil && strings.TrimSpace(payload.MessageID) != "" {
				return events.NewMessageDelta(messageIDs.ForProvider(payload.MessageID), payload.Content)
			}
			if err == nil {
				return events.NewMessageDelta(messageIDs.CurrentOrNew(), payload.Content)
			}
			return &event
		}
		return events.NewMessageDelta(messageIDs.CurrentOrNew(), event.Content)
	case events.EventReasoningDelta:
		if len(event.Payload) > 0 {
			payload, err := events.DecodePayload[events.MessageDeltaPayload](&event)
			if err == nil && strings.TrimSpace(payload.MessageID) != "" {
				return events.NewReasoningDelta(messageIDs.ForProvider(payload.MessageID), payload.Content)
			}
			if err == nil {
				return events.NewReasoningDelta(messageIDs.CurrentOrNew(), payload.Content)
			}
			return &event
		}
		return events.NewReasoningDelta(messageIDs.CurrentOrNew(), event.Content)
	case events.EventToolCallStarted:
		payload, err := events.DecodePayload[events.ToolCallPayload](&event)
		if err != nil {
			return &event
		}
		return events.NewToolCallStarted(firstNonEmptyString(payload.ToolCallID, legacyToolCallID(event)), payload.Name, payload.Arguments)
	case events.EventToolCallCompleted:
		payload, err := events.DecodePayload[events.ToolCallResultPayload](&event)
		if err != nil {
			return &event
		}
		return events.NewToolCallCompleted(payload.ToolCallID, payload.Name, payload.Result, payload.ElapsedMS)
	case events.EventToolCallFailed:
		payload, err := events.DecodePayload[events.ToolCallResultPayload](&event)
		if err != nil {
			return &event
		}
		return events.NewToolCallFailed(payload.ToolCallID, payload.Name, payload.Error, payload.ElapsedMS)
	default:
		return &event
	}
}

func legacyToolCallID(event events.Event) string {
	var payload struct {
		CallID     string `json:"call_id"`
		ToolCallID string `json:"tool_call_id"`
	}
	if len(event.Payload) > 0 && json.Unmarshal(event.Payload, &payload) == nil {
		return firstNonEmptyString(payload.ToolCallID, payload.CallID)
	}
	if strings.TrimSpace(event.Content) != "" && json.Unmarshal([]byte(event.Content), &payload) == nil {
		return firstNonEmptyString(payload.ToolCallID, payload.CallID)
	}
	return ""
}

type providerSessionPlan struct {
	InternalSessionID string
	ProviderSessionID string
	Resume            bool
	Key               ProviderSessionKey
}

func (r *Runner) resolveProviderSession(ctx context.Context, req *agent.RequestContext, workDir string) providerSessionPlan {
	internalSessionID := internalSessionIDFromRequest(req)
	plan := providerSessionPlan{
		InternalSessionID: internalSessionID,
		Key: ProviderSessionKey{
			InternalSessionID: internalSessionID,
			Provider:          r.name,
			WorkDir:           workDir,
			AssistantID:       req.Assistant.ID,
		},
	}
	if internalSessionID == "" || r.sessionStore == nil {
		return plan
	}
	binding, err := r.sessionStore.Get(ctx, plan.Key)
	if err != nil {
		logs.WarnContextf(ctx, "Resolve provider session failed: provider=%s session=%s error=%v", r.name, internalSessionID, err)
		return plan
	}
	if binding != nil && strings.TrimSpace(binding.ProviderSessionID) != "" && binding.Status != providerSessionStatusFailed {
		plan.ProviderSessionID = strings.TrimSpace(binding.ProviderSessionID)
		plan.Resume = true
		return plan
	}
	return plan
}

func (r *Runner) persistProviderSession(ctx context.Context, plan providerSessionPlan, observedProviderSessionID string) {
	if r.sessionStore == nil || plan.InternalSessionID == "" {
		return
	}
	providerSessionID := firstNonEmptyString(observedProviderSessionID, plan.ProviderSessionID)
	if providerSessionID == "" {
		return
	}
	if plan.Resume && providerSessionID == plan.ProviderSessionID {
		return
	}
	if err := r.sessionStore.Upsert(ctx, &ProviderSessionBinding{
		InternalSessionID: plan.InternalSessionID,
		Provider:          plan.Key.Provider,
		ProviderSessionID: providerSessionID,
		WorkDir:           plan.Key.WorkDir,
		AssistantID:       plan.Key.AssistantID,
		Status:            providerSessionStatusActive,
	}); err != nil {
		logs.WarnContextf(ctx, "Store provider session failed: provider=%s session=%s provider_session=%s error=%v", plan.Key.Provider, plan.InternalSessionID, providerSessionID, err)
	}
}

func (r *Runner) markProviderSessionFailed(ctx context.Context, plan providerSessionPlan, runErr error) {
	if r.sessionStore == nil || plan.InternalSessionID == "" || plan.ProviderSessionID == "" || runErr == nil {
		return
	}
	if err := r.sessionStore.MarkFailed(ctx, plan.Key, runErr.Error()); err != nil {
		logs.WarnContextf(ctx, "Mark provider session failed: provider=%s session=%s error=%v", plan.Key.Provider, plan.InternalSessionID, err)
	}
}

func internalSessionIDFromRequest(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.Conversation.ID)
}

func metadataString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r *Runner) failedResult(req *agent.RequestContext, startedAt time.Time, err error, metadata map[string]any) *agent.RunResult {
	status := agent.RunStatusFailed
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = agent.RunStatusCancelled
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	return &agent.RunResult{
		RunID:       req.RunID,
		TraceID:     req.TraceID,
		Status:      status,
		Error:       message,
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
		Metadata:    metadataWithRuntime(metadata, r.name),
	}
}

func failureMetadata(workDir string) map[string]any {
	metadata := map[string]any{}
	if strings.TrimSpace(workDir) != "" {
		metadata["work_dir"] = workDir
	}
	return metadata
}

func metadataWithRuntime(metadata map[string]any, runtimeName string) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["runtime"] = runtimeName
	return metadata
}

func sinkForRequest(req *agent.RequestContext) events.Sink {
	if req == nil || req.EventSink == nil {
		return events.NewNoopSink()
	}
	return req.EventSink
}

func modelForRequest(req *agent.RequestContext) engines.ModelConfig {
	if req == nil {
		return engines.ModelConfig{}
	}
	model := engines.ModelConfig{
		Provider: req.Model.Provider,
		Model:    req.Model.Model,
		APIKey:   req.Model.APIKey,
		BaseURL:  req.Model.BaseURL,
	}
	return model
}

var _ agent.Runner = (*Runner)(nil)
