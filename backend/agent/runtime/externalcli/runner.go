// Package externalcli adapts external agent CLI providers to the agent.Runtime contract.
package externalcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/agent/runtime/provider"
	runtimetodo "github.com/insmtx/Leros/backend/agent/runtime/todo"
	"github.com/ygpkg/yg-go/logs"
)

// Driver contains the shared process, provider-session, and event parsing machinery
// used by concrete CLI Runtime implementations.
type Driver struct {
	name            string
	invoker         Invoker
	sessionStore    ProviderSessionStore
	approvalHandler provider.ApprovalHandler
	questionHandler provider.QuestionHandler
	mcpServers      []provider.MCPServerConfig
}

// NewDriver creates shared infrastructure for one concrete CLI Runtime.
func NewDriver(name string, invoker Invoker, options ...DriverOptions) (*Driver, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("runtime name is required")
	}
	if invoker == nil {
		return nil, fmt.Errorf("runtime %q invoker is nil", name)
	}
	driver := &Driver{
		name:         name,
		invoker:      invoker,
		sessionStore: NewInMemoryProviderSessionStore(),
	}
	if len(options) > 0 {
		option := options[0]
		if option.SessionStore != nil {
			driver.sessionStore = option.SessionStore
		}
		driver.approvalHandler = option.ApprovalHandler
		driver.questionHandler = option.QuestionHandler
		driver.mcpServers = append([]provider.MCPServerConfig(nil), option.MCPServers...)
	}
	return driver, nil
}

// RunInvocation executes one request for the concrete Runtime that owns this
// provider invocation facility.
func (r *Driver) RunInvocation(
	ctx context.Context,
	request agent.ExecutionRequest,
	observer agent.Observer,
) (agent.ExecutionResult, error) {
	if r == nil || r.invoker == nil {
		return agent.ExecutionResult{}, fmt.Errorf("external CLI runtime is not initialized")
	}
	if strings.TrimSpace(request.ExecutionID) == "" {
		return agent.ExecutionResult{}, fmt.Errorf("execution id is required")
	}
	var eventSink events.Sink = events.NewNoopSink()
	if observer != nil {
		eventSink = observer
	}
	workDir := strings.TrimSpace(request.Filesystem.WorkDir)
	if err := r.invoker.Prepare(ctx, workDir); err != nil {
		return agent.ExecutionResult{}, err
	}

	sessionPlan := r.resolveProviderSession(ctx, request)
	handle, err := r.invoker.Invoke(ctx, InvocationRequest{
		ExecutionID:     request.ExecutionID,
		ExecutionMode:   request.Mode,
		SessionID:       sessionPlan.ProviderSessionID,
		Resume:          sessionPlan.Resume,
		WorkDir:         workDir,
		TaskDir:         request.Filesystem.TaskDir,
		SystemPrompt:    strings.TrimSpace(request.SystemPrompt),
		Prompt:          request.Prompt,
		Messages:        append([]agent.Message(nil), request.Messages...),
		Tools:           append([]agent.Tool(nil), request.Tools...),
		AllowedTools:    append([]string(nil), request.Policy.AllowedTools...),
		TraceID:         request.TraceID,
		SessionKey:      request.SessionKey,
		Model:           request.Model,
		ExtraEnv:        nil,
		PermissionMode:  provider.PermissionMode(request.Policy.PermissionMode),
		ApprovalHandler: r.approvalHandler,
		MCPServers:      r.mcpServers,
	})
	if err != nil {
		return agent.ExecutionResult{}, err
	}

	if handle != nil && handle.Process != nil {
		logs.InfoContextf(ctx, "External runtime %s started with pid %d", r.name, handle.Process.PID())
	}

	consumeResult, err := ConsumeEvents(
		ctx,
		eventSink,
		handle,
		request.ExecutionID,
		request.TraceID,
		r.approvalHandler,
		r.questionHandler,
	)
	if err != nil {
		r.markProviderSessionFailed(ctx, sessionPlan, err)
		return agent.ExecutionResult{}, err
	}
	r.persistProviderSession(ctx, sessionPlan, consumeResult.ProviderSessionID)

	return agent.ExecutionResult{
		Message:                strings.TrimSpace(consumeResult.Message),
		Usage:                  consumeResult.Usage,
		ProviderConversationID: firstNonEmptyString(sessionPlan.ProviderSessionID, consumeResult.ProviderSessionID),
	}, nil
}

// ConsumeResult is the provider-independent result extracted from an event stream.
type ConsumeResult struct {
	Message           string
	ProviderSessionID string
	Usage             *agent.Usage
}

// ConsumeEvents parses provider activity events and forwards normalized activity to sink.
func ConsumeEvents(
	ctx context.Context,
	sink events.Sink,
	handle *Invocation,
	runID string,
	traceID string,
	approvalHandler provider.ApprovalHandler,
	questionHandler provider.QuestionHandler,
) (ConsumeResult, error) {
	if handle == nil || handle.Events == nil {
		return ConsumeResult{}, nil
	}
	if sink == nil {
		sink = events.NewNoopSink()
	}
	var result strings.Builder
	resultSeen := false
	consumed := ConsumeResult{}
	messageIDs := events.NewMessageIDMapper()
	emit := func(event *agent.Event) error {
		if event == nil {
			return nil
		}
		if err := sink.Emit(ctx, event); err != nil {
			return fmt.Errorf("emit %s: %w", event.Type, err)
		}
		return nil
	}
	todoSink := events.SinkFunc(func(emitCtx context.Context, event *agent.Event) error {
		if event == nil {
			return nil
		}
		if event.RunID == "" {
			event.RunID = runID
		}
		if event.TraceID == "" {
			event.TraceID = traceID
		}
		return sink.Emit(emitCtx, event)
	})
	todoTracker := runtimetodo.NewTracker(runtimetodo.Options{RunID: runID, Sink: todoSink})
	for event := range handle.Events {
		// Fill RunID/TraceID that the old Journal would have provided.
		if event.RunID == "" {
			event.RunID = runID
		}
		if event.TraceID == "" {
			event.TraceID = traceID
		}
		switch event.Type {
		case events.EventInvocationStarted:
			continue
		case events.EventProviderSessionStarted:
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
		case events.EventInvocationCompleted:
			consumed.Message = result.String()
			return consumed, nil
		case events.EventInvocationFailed:
			if strings.TrimSpace(event.Content) == "" {
				consumed.Message = result.String()
				return consumed, fmt.Errorf("external runtime failed")
			}
			consumed.Message = result.String()
			return consumed, fmt.Errorf("%s", event.Content)
		case events.EventInvocationCancelled:
			consumed.Message = result.String()
			if ctx.Err() != nil {
				return consumed, ctx.Err()
			}
			return consumed, context.Canceled
		case events.EventMessageDelta:
			if strings.TrimSpace(event.Content) != "" {
				if err := emit(normalizeRuntimeEvent(event, messageIDs)); err != nil {
					return consumed, err
				}
				if !resultSeen {
					result.WriteString(event.Content)
				}
			}
		case events.EventReasoningDelta:
			if err := emit(normalizeRuntimeEvent(event, messageIDs)); err != nil {
				return consumed, err
			}
		case events.EventToolCallStarted, events.EventToolCallCompleted, events.EventToolCallFailed:
			if err := emit(normalizeRuntimeEvent(event, messageIDs)); err != nil {
				return consumed, err
			}
		case events.EventTodoSnapshot:
			if items, err := events.DecodePayload[[]events.RuntimeTodoItem](&event); err == nil {
				if err := todoTracker.Snapshot(ctx, items); err != nil {
					return consumed, fmt.Errorf("emit %s: %w", event.Type, err)
				}
			}
		case events.EventTodoUpdated:
			if items, err := events.DecodePayload[[]events.RuntimeTodoItem](&event); err == nil {
				if err := todoTracker.Update(ctx, items, true); err != nil {
					return consumed, fmt.Errorf("emit %s: %w", event.Type, err)
				}
			}
		case events.EventApprovalRequested:
			if err := emit(normalizeRuntimeEvent(event, messageIDs)); err != nil {
				return consumed, err
			}
			if handle.Responder == nil {
				logs.WarnContextf(ctx, "approval request dropped: no Responder (PermissionMode may need to be on-request/auto)")
			}
			if approvalHandler != nil && handle.Responder != nil {
				req, decErr := events.DecodePayload[events.ApprovalRequestPayload](&event)
				if decErr != nil {
					logs.WarnContextf(ctx, "decode approval request: %v", decErr)
					continue
				}
				decision, decErr := approvalHandler.RequestApproval(ctx, &agent.ApprovalRequest{
					RequestID:   req.RequestID,
					ToolCallID:  req.ToolCallID,
					ToolName:    req.ToolName,
					Arguments:   json.RawMessage(req.Arguments),
					Description: req.Description,
					Runtime:     metadataString(req.Metadata, "engine"),
				})
				if decErr != nil {
					logs.WarnContextf(ctx, "approval handler error: %v", decErr)
					continue
				}
				if wErr := handle.Responder.WriteDecision(req.RequestID, decision.Action); wErr != nil {
					logs.WarnContextf(ctx, "write approval decision to stdin: %v", wErr)
				}
				if err := emit(normalizeRuntimeEvent(*events.NewApprovalResolved(events.ApprovalDecisionPayload{
					RequestID: req.RequestID,
					Action:    decision.Action,
					Reason:    decision.Reason,
				}), messageIDs)); err != nil {
					return consumed, err
				}
			}
		case events.EventApprovalResolved:
			if err := emit(normalizeRuntimeEvent(event, messageIDs)); err != nil {
				return consumed, err
			}
		case events.EventQuestionAsked:
			if err := emit(normalizeRuntimeEvent(event, messageIDs)); err != nil {
				return consumed, err
			}
			if handle.Questions == nil {
				logs.WarnContextf(ctx, "question request dropped: no QuestionResponder")
			}
			if questionHandler != nil && handle.Questions != nil {
				req, decErr := events.DecodePayload[events.QuestionRequestPayload](&event)
				if decErr != nil {
					logs.WarnContextf(ctx, "decode question request: %v", decErr)
					continue
				}
				// 构建 engine 层的 QuestionRequest
				qItems := make([]agent.QuestionItem, 0, len(req.Questions))
				for _, q := range req.Questions {
					opts := make([]agent.QuestionOption, 0, len(q.Options))
					for _, o := range q.Options {
						opts = append(opts, agent.QuestionOption{
							Label:       o.Label,
							Description: o.Description,
						})
					}
					qItems = append(qItems, agent.QuestionItem{
						Question:    q.Question,
						Header:      q.Header,
						Options:     opts,
						MultiSelect: q.MultiSelect,
						Custom:      q.Custom,
					})
				}
				answer, decErr := questionHandler.RequestAnswer(ctx, &agent.QuestionRequest{
					RequestID:   req.RequestID,
					SessionKey:  req.SessionID,
					Questions:   qItems,
					ToolCallID:  req.ToolCallID,
					Description: firstQuestionText(req.Questions),
					Runtime:     metadataString(req.Metadata, "engine"),
				})
				if decErr != nil {
					logs.WarnContextf(ctx, "question handler error: %v", decErr)
					continue
				}
				if wErr := handle.Questions.WriteAnswer(req.RequestID, answer.Answers); wErr != nil {
					logs.WarnContextf(ctx, "write question answer: %v", wErr)
				}
				if err := emit(normalizeRuntimeEvent(*events.NewQuestionAnswered(events.QuestionAnswerPayload{
					RequestID: req.RequestID,
					Answers:   answer.Answers,
				}), messageIDs)); err != nil {
					return consumed, err
				}
			}
		case events.EventQuestionAnswered:
			if err := emit(normalizeRuntimeEvent(event, messageIDs)); err != nil {
				return consumed, err
			}
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

func normalizeRuntimeEvent(event agent.Event, messageIDs *events.MessageIDMapper) *agent.Event {
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

func legacyToolCallID(event agent.Event) string {
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

func (r *Driver) resolveProviderSession(ctx context.Context, request agent.ExecutionRequest) providerSessionPlan {
	internalSessionID := strings.TrimSpace(request.SessionKey)
	plan := providerSessionPlan{
		InternalSessionID: internalSessionID,
		Key: ProviderSessionKey{
			InternalSessionID: internalSessionID,
			Provider:          r.name,
			WorkDir:           request.Filesystem.WorkDir,
			AssistantID:       request.InstanceKey,
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

func (r *Driver) persistProviderSession(ctx context.Context, plan providerSessionPlan, observedProviderSessionID string) {
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

func (r *Driver) markProviderSessionFailed(ctx context.Context, plan providerSessionPlan, runErr error) {
	if r.sessionStore == nil || plan.InternalSessionID == "" || plan.ProviderSessionID == "" || runErr == nil {
		return
	}
	if err := r.sessionStore.MarkFailed(ctx, plan.Key, runErr.Error()); err != nil {
		logs.WarnContextf(ctx, "Mark provider session failed: provider=%s session=%s error=%v", plan.Key.Provider, plan.InternalSessionID, err)
	}
}

func metadataString(meta map[string]string, key string) string {
	if meta == nil {
		return ""
	}
	return meta[key]
}

func firstQuestionText(questions []events.QuestionItem) string {
	if len(questions) == 0 {
		return ""
	}
	if questions[0].Header != "" {
		return questions[0].Header
	}
	return questions[0].Question
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
