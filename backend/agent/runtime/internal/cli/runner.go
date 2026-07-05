// Package cli provides shared CLI process infrastructure for external agent runtimes.
package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	runtimetodo "github.com/insmtx/Leros/backend/agent/runtime/internal/todo"
	"github.com/ygpkg/yg-go/logs"
)

// Driver contains the shared process, provider-session, and event parsing machinery
// used by concrete CLI Runtime implementations.
type Driver struct {
	name               string
	invoker            Invoker
	interactionHandler agent.InteractionHandler
	mcpServers         []agent.MCPServerConfig
}

// NewDriver creates shared infrastructure for one concrete CLI Runtime.
func NewDriver(
	name string,
	invoker Invoker,
	options ...agent.RuntimeAdapterOptions,
) (*Driver, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("runtime name is required")
	}
	if invoker == nil {
		return nil, fmt.Errorf("runtime %q invoker is nil", name)
	}
	driver := &Driver{
		name:    name,
		invoker: invoker,
	}
	if len(options) > 0 {
		option := options[0]
		driver.interactionHandler = option.InteractionHandler
		driver.mcpServers = append([]agent.MCPServerConfig(nil), option.MCPServers...)
	}
	return driver, nil
}

// RunInvocation executes one request for the concrete Runtime that owns this
// provider invocation facility.
func (r *Driver) RunInvocation(
	ctx context.Context,
	request agent.ExecutionRequest,
	observer agent.NodeObserver,
) (agent.ExecutionResult, error) {
	if r == nil || r.invoker == nil {
		return agent.ExecutionResult{}, fmt.Errorf("external CLI runtime is not initialized")
	}
	if strings.TrimSpace(request.ExecutionID) == "" {
		return agent.ExecutionResult{}, fmt.Errorf("execution id is required")
	}
	workDir := strings.TrimSpace(request.Filesystem.WorkDir)
	if err := r.invoker.Prepare(ctx, workDir); err != nil {
		return agent.ExecutionResult{}, err
	}

	providerSessionID := request.ProviderSession.ID
	resumeSession := request.ProviderSession.Resume

	handle, err := r.invoker.Invoke(ctx, InvocationRequest{
		ExecutionID:     request.ExecutionID,
		ExecutionMode:   request.Mode,
		SessionID:       providerSessionID,
		Resume:          resumeSession,
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
		PermissionMode:  request.Policy.PermissionMode,
		ApprovalHandler: r.interactionHandler,
		MCPServers:      r.mcpServers,
	})
	if err != nil {
		return agent.ExecutionResult{}, err
	}

	if handle != nil && handle.Process != nil {
		logs.InfoContextf(ctx, "External runtime %s started with pid %d", r.name, handle.Process.PID())
	}

	invocationResult, err := ConsumeEvents(
		ctx,
		observer,
		handle,
		request.ExecutionID,
		request.TraceID,
		r.interactionHandler,
		r.interactionHandler,
	)
	if err != nil {
		return agent.ExecutionResult{}, err
	}

	return agent.ExecutionResult{
		Message:                strings.TrimSpace(invocationResult.Message),
		Usage:                  agent.EnsureUsage(invocationResult.Usage),
		ProviderConversationID: firstNonEmptyString(providerSessionID, invocationResult.ProviderSessionID),
	}, nil
}

// ConsumeEvents reads NodeEvents from the invocation, processes approvals/questions,
// normalizes message IDs, and forwards to the NodeObserver.
func ConsumeEvents(
	ctx context.Context,
	observer agent.NodeObserver,
	handle *Invocation,
	executionID string,
	traceID string,
	approvalHandler agent.InteractionHandler,
	questionHandler agent.InteractionHandler,
) (InvocationResult, error) {
	if handle == nil || handle.Events == nil {
		return InvocationResult{}, fmt.Errorf("invocation event stream is required")
	}
	observedProviderSessionID := ""
	messageIDs := NewMessageIDMapper()
	emit := func(event agent.NodeEvent) error {
		if observer == nil {
			return nil
		}
		event.ExecutionID = executionID
		event.TraceID = traceID
		if err := observer.Observe(ctx, event); err != nil {
			return fmt.Errorf("emit %s: %w", event.Type, err)
		}
		return nil
	}
	todoTracker := runtimetodo.NewTracker(runtimetodo.Options{
		RunID: executionID,
		Observer: agent.NodeObserverFunc(func(ctx context.Context, event agent.NodeEvent) error {
			return emit(event)
		}),
	})
	for raw := range handle.Events {
		event := normalizeRuntimeEvent(raw, messageIDs)
		event.ExecutionID = executionID
		event.TraceID = traceID
		switch event.Type {
		case agent.NodeEventAgentStart:
			if p, ok := event.Payload.(*agent.AgentStartedPayload); ok && strings.TrimSpace(p.ProviderSessionID) != "" {
				observedProviderSessionID = strings.TrimSpace(p.ProviderSessionID)
			}
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
		case agent.NodeEventMessageEnd:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
		case agent.NodeEventMessageUpdate:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
		case agent.NodeEventReasoningUpdate:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
		case agent.NodeEventToolExecutionStart, agent.NodeEventToolExecutionEnd:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
		case agent.NodeEventTodoSnapshot:
			if p, ok := event.Payload.(*agent.TodoSnapshotPayload); ok {
				if err := todoTracker.Snapshot(ctx, p.Items); err != nil {
					return InvocationResult{}, fmt.Errorf("emit %s: %w", event.Type, err)
				}
			}
		case agent.NodeEventTodoUpdated:
			if p, ok := event.Payload.(*agent.TodoUpdatedPayload); ok {
				if err := todoTracker.Update(ctx, p.Items, true); err != nil {
					return InvocationResult{}, fmt.Errorf("emit %s: %w", event.Type, err)
				}
			}
		case agent.NodeEventApprovalRequested:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
			if handle.Responder == nil {
				logs.WarnContextf(ctx, "approval request dropped: no Responder (PermissionMode may need to be on-request/auto)")
			}
			if approvalHandler != nil && handle.Responder != nil {
				p, ok := event.Payload.(*agent.ApprovalRequestedPayload)
				if !ok {
					logs.WarnContextf(ctx, "approval request payload type mismatch")
					continue
				}
				decision, decErr := approvalHandler.RequestApproval(ctx, &agent.ApprovalRequest{
					RequestID:   p.RequestID,
					ToolCallID:  p.ToolCallID,
					ToolName:    p.ToolName,
					Arguments:   p.Arguments,
					Description: p.Description,
					Runtime:     metadataString(p.Metadata, "engine"),
				})
				if decErr != nil {
					logs.WarnContextf(ctx, "approval handler error: %v", decErr)
					continue
				}
				if wErr := handle.Responder.WriteDecision(p.RequestID, decision.Action); wErr != nil {
					logs.WarnContextf(ctx, "write approval decision to stdin: %v", wErr)
				}
				if err := emit(normalizeRuntimeEvent(agent.NewApprovalResolvedEvent(agent.ApprovalResolvedPayload{
					RequestID: p.RequestID,
					Action:    decision.Action,
					Reason:    decision.Reason,
				}), messageIDs)); err != nil {
					return InvocationResult{}, err
				}
			}
		case agent.NodeEventApprovalResolved:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
		case agent.NodeEventQuestionAsked:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
			if handle.Questions == nil {
				logs.WarnContextf(ctx, "question request dropped: no QuestionResponder")
			}
			if questionHandler != nil && handle.Questions != nil {
				p, ok := event.Payload.(*agent.QuestionAskedPayload)
				if !ok {
					logs.WarnContextf(ctx, "question request payload type mismatch")
					continue
				}
				qItems := make([]agent.QuestionItem, 0, len(p.Questions))
				for _, q := range p.Questions {
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
					RequestID:   p.RequestID,
					SessionKey:  p.SessionID,
					Questions:   qItems,
					ToolCallID:  p.ToolCallID,
					Description: firstQuestionText(p.Questions),
					Runtime:     metadataString(p.Metadata, "engine"),
				})
				if decErr != nil {
					logs.WarnContextf(ctx, "question handler error: %v", decErr)
					continue
				}
				if wErr := handle.Questions.WriteAnswer(p.RequestID, answer.Answers); wErr != nil {
					logs.WarnContextf(ctx, "write question answer: %v", wErr)
				}
				if err := emit(normalizeRuntimeEvent(agent.NewQuestionAnsweredEvent(agent.QuestionAnsweredPayload{
					RequestID: p.RequestID,
					Answers:   answer.Answers,
				}), messageIDs)); err != nil {
					return InvocationResult{}, err
				}
			}
		case agent.NodeEventQuestionAnswered:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
		case agent.NodeEventPlanReady:
			if err := emit(event); err != nil {
				return InvocationResult{}, err
			}
		default:
			// unknown event types are silently consumed
		}
	}
	if handle.Result == nil {
		return InvocationResult{}, fmt.Errorf("invocation result stream is required")
	}
	var result InvocationResult
	select {
	case <-ctx.Done():
		return InvocationResult{}, ctx.Err()
	case terminal, ok := <-handle.Result:
		if !ok {
			return InvocationResult{}, fmt.Errorf("invocation result stream closed without result")
		}
		result = terminal
	}
	result.Usage = agent.EnsureUsage(result.Usage)
	if result.ProviderSessionID == "" {
		result.ProviderSessionID = observedProviderSessionID
	}
	if result.Err != nil {
		return result, result.Err
	}
	return result, nil
}

// normalizeRuntimeEvent rewrites message IDs for consistency and normalizes payloads.
func normalizeRuntimeEvent(event agent.NodeEvent, messageIDs *MessageIDMapper) agent.NodeEvent {
	switch event.Type {
	case agent.NodeEventMessageUpdate:
		if p, ok := event.Payload.(*agent.MessageUpdatePayload); ok && strings.TrimSpace(p.MessageID) != "" {
			event.Payload = &agent.MessageUpdatePayload{
				MessageID: messageIDs.ForProvider(p.MessageID),
				Role:      p.Role,
				Content:   p.Content,
			}
		} else {
			event.Payload = &agent.MessageUpdatePayload{
				MessageID: messageIDs.CurrentOrNew(),
				Role:      "assistant",
				Content:   "",
			}
		}
	case agent.NodeEventReasoningUpdate:
		if p, ok := event.Payload.(*agent.ReasoningUpdatePayload); ok && strings.TrimSpace(p.MessageID) != "" {
			event.Payload = &agent.ReasoningUpdatePayload{
				MessageID: messageIDs.ForProvider(p.MessageID),
				Content:   p.Content,
			}
		} else {
			event.Payload = &agent.ReasoningUpdatePayload{
				MessageID: messageIDs.CurrentOrNew(),
				Content:   "",
			}
		}
	}
	return event
}

func metadataString(meta map[string]string, key string) string {
	if meta == nil {
		return ""
	}
	return meta[key]
}

func firstQuestionText(questions []agent.QuestionItem) string {
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
