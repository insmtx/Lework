package agentrun

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

// NodeHandler owns the Worker-side interpretation of runtime NodeEvents.
type NodeHandler struct {
	journal       Journal
	planPublisher PlanPublisher
	sessionStore  ProviderSessionStore
	provider      string
	sessionKey    string
	planErr       error
}

// NewNodeHandler creates the business handler used as the Runtime observer.
func NewNodeHandler(
	journal Journal,
	planPublisher PlanPublisher,
	sessionStore ProviderSessionStore,
	provider string,
	sessionKey string,
) *NodeHandler {
	return &NodeHandler{
		journal:       journal,
		planPublisher: planPublisher,
		sessionStore:  sessionStore,
		provider:      strings.TrimSpace(provider),
		sessionKey:    sessionKey,
	}
}

// Observe handles internal nodes and maps externally visible nodes to RunEventBody.
func (h *NodeHandler) Observe(ctx context.Context, event agent.NodeEvent) error {
	if h == nil {
		return nil
	}
	switch event.Type {
	case agent.NodeEventAgentStart:
		h.handleAgentStart(ctx, event)
		return nil
	case agent.NodeEventPlanReady:
		return h.handlePlanReady(ctx, event)
	}

	body, ok, err := runEventBodyFromNode(event)
	if err != nil {
		return err
	}
	if !ok || h.journal == nil {
		return nil
	}
	return h.journal.Record(ctx, RunEventDraft{
		OccurredAt: event.OccurredAt,
		Body:       body,
	})
}

// PlanError returns a plan publication error captured during runtime observation.
func (h *NodeHandler) PlanError() error {
	if h == nil {
		return nil
	}
	return h.planErr
}

func (h *NodeHandler) handlePlanReady(ctx context.Context, event agent.NodeEvent) error {
	if h.planPublisher == nil {
		return nil
	}
	body, err := h.planPublisher.Publish(ctx, event)
	if err != nil {
		if _, ok := err.(*PlanPublishError); ok {
			h.planErr = err
			logs.WarnContextf(ctx, "[plan] publish captured for business finalization: %v", err)
			return nil
		}
		return fmt.Errorf("plan publisher: %w", err)
	}
	if body == nil || h.journal == nil {
		return nil
	}
	return h.journal.Record(ctx, RunEventDraft{
		OccurredAt: event.OccurredAt,
		Body:       *body,
	})
}

func (h *NodeHandler) handleAgentStart(ctx context.Context, event agent.NodeEvent) {
	if h.sessionStore == nil {
		return
	}
	payload, ok := agentStartPayload(event.Payload)
	if !ok || strings.TrimSpace(payload.ProviderSessionID) == "" {
		return
	}
	provider := h.provider
	if provider == "" {
		provider = "unknown"
	}
	internalKey := h.sessionKey
	if internalKey == "" {
		// Fallback to ExecutionID for backward compatibility
		internalKey = event.ExecutionID
	}
	if err := h.sessionStore.UpsertProviderSession(ctx, &ProviderSessionBinding{
		InternalSessionID: internalKey,
		Provider:          provider,
		ProviderSessionID: payload.ProviderSessionID,
		Status:            "active",
	}); err != nil {
		logs.WarnContextf(
			ctx,
			"Upsert provider session failed: provider=%s session=%s error=%v",
			provider,
			internalKey,
			err,
		)
	}
}

func runEventBodyFromNode(event agent.NodeEvent) (messaging.RunEventBody, bool, error) {
	body := messaging.RunEventBody{}
	switch event.Type {
	case agent.NodeEventMessageUpdate:
		payload, ok := messageUpdatePayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventMessageDelta
		body.Payload.MessageID = payload.MessageID
		body.Payload.Role = messaging.MessageRole(payload.Role)
		body.Payload.Content = payload.Content
	case agent.NodeEventMessageEnd:
		payload, ok := messageEndPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventMessageCompleted
		body.Payload.MessageID = payload.MessageID
		body.Payload.Role = messaging.MessageRoleAssistant
		body.Payload.Content = payload.Content
		body.Payload.Usage = agentUsageToMessaging(payload.Usage)
	case agent.NodeEventReasoningUpdate:
		payload, ok := reasoningUpdatePayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventReasoningDelta
		body.Payload.MessageID = payload.MessageID
		body.Payload.Role = messaging.MessageRoleAssistant
		body.Payload.Content = payload.Content
	case agent.NodeEventToolExecutionStart:
		payload, ok := toolExecutionStartPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventToolCallStarted
		body.Payload.ToolCall = &messaging.ToolCallPayload{
			ToolCallID: payload.ToolCallID,
			Name:       payload.Name,
			Arguments:  append([]byte(nil), payload.Arguments...),
		}
	case agent.NodeEventToolExecutionEnd:
		payload, ok := toolExecutionEndPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventToolCallFinished
		body.Payload.ToolResult = &messaging.ToolCallResultPayload{
			ToolCallID: payload.ToolCallID,
			Name:       payload.Name,
			IsError:    payload.IsError,
			Error:      payload.Error,
			Result:     append([]byte(nil), payload.Result...),
			ElapsedMS:  payload.ElapsedMS,
		}
	case agent.NodeEventTodoSnapshot:
		payload, ok := todoSnapshotPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventTodoSnapshot
		body.Payload.Todos = runtimeTodosToMessaging(payload.Items)
	case agent.NodeEventTodoUpdated:
		payload, ok := todoUpdatedPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventTodoUpdated
		body.Payload.Todos = runtimeTodosToMessaging(payload.Items)
	case agent.NodeEventApprovalRequested:
		payload, ok := approvalRequestedPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventApprovalRequested
		body.Payload.ApprovalRequest = &messaging.ApprovalRequestPayload{
			RequestID: payload.RequestID, ToolName: payload.ToolName,
			ToolCallID: payload.ToolCallID, Description: payload.Description,
			Arguments: append([]byte(nil), payload.Arguments...),
			Metadata:  cloneStringMap(payload.Metadata),
		}
	case agent.NodeEventApprovalResolved:
		payload, ok := approvalResolvedPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventApprovalResolved
		body.Payload.ApprovalDecision = &messaging.ApprovalDecisionPayload{
			RequestID: payload.RequestID,
			Action:    payload.Action,
			Reason:    payload.Reason,
		}
	case agent.NodeEventQuestionAsked:
		payload, ok := questionAskedPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventQuestionAsked
		body.Payload.QuestionRequest = questionRequestToMessaging(payload)
	case agent.NodeEventQuestionAnswered:
		payload, ok := questionAnsweredPayload(event.Payload)
		if !ok {
			return body, false, payloadTypeError(event)
		}
		body.Event = messaging.RunEventQuestionAnswered
		body.Payload.QuestionAnswer = &messaging.QuestionAnswerPayload{
			RequestID: payload.RequestID,
			Answers:   cloneAnswers(payload.Answers),
		}
	case agent.NodeEventToolExecutionUpdate:
		// The public protocol intentionally has no high-frequency tool update event.
		return body, false, nil
	default:
		return body, false, nil
	}
	return body, true, nil
}

func payloadTypeError(event agent.NodeEvent) error {
	return fmt.Errorf("node event %s has payload %T", event.Type, event.Payload)
}

func runtimeTodosToMessaging(items []agent.RuntimeTodoItem) []messaging.RuntimeTodoItem {
	result := make([]messaging.RuntimeTodoItem, len(items))
	for index, item := range items {
		result[index] = messaging.RuntimeTodoItem{
			ID: item.ID, Title: item.Title, Status: item.Status, Priority: item.Priority,
		}
	}
	return result
}

func questionRequestToMessaging(payload agent.QuestionAskedPayload) *messaging.QuestionRequestPayload {
	result := &messaging.QuestionRequestPayload{
		RequestID: payload.RequestID, SessionID: payload.SessionID,
		ToolCallID: payload.ToolCallID, MessageID: payload.MessageID,
		InteractionType: payload.InteractionType, Metadata: cloneStringMap(payload.Metadata),
	}
	for _, question := range payload.Questions {
		item := messaging.QuestionItem{
			Question: question.Question, Header: question.Header,
			MultiSelect: question.MultiSelect, Custom: question.Custom,
		}
		for _, option := range question.Options {
			item.Options = append(item.Options, messaging.QuestionOption{
				Label: option.Label, Description: option.Description,
			})
		}
		result.Questions = append(result.Questions, item)
	}
	return result
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneAnswers(source [][]string) [][]string {
	result := make([][]string, len(source))
	for index := range source {
		result[index] = append([]string(nil), source[index]...)
	}
	return result
}

func messageUpdatePayload(payload agent.NodeEventPayload) (agent.MessageUpdatePayload, bool) {
	switch value := payload.(type) {
	case agent.MessageUpdatePayload:
		return value, true
	case *agent.MessageUpdatePayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.MessageUpdatePayload{}, false
}

func messageEndPayload(payload agent.NodeEventPayload) (agent.MessageEndPayload, bool) {
	switch value := payload.(type) {
	case agent.MessageEndPayload:
		return value, true
	case *agent.MessageEndPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.MessageEndPayload{}, false
}

func reasoningUpdatePayload(payload agent.NodeEventPayload) (agent.ReasoningUpdatePayload, bool) {
	switch value := payload.(type) {
	case agent.ReasoningUpdatePayload:
		return value, true
	case *agent.ReasoningUpdatePayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.ReasoningUpdatePayload{}, false
}

func toolExecutionStartPayload(payload agent.NodeEventPayload) (agent.ToolExecutionStartPayload, bool) {
	switch value := payload.(type) {
	case agent.ToolExecutionStartPayload:
		return value, true
	case *agent.ToolExecutionStartPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.ToolExecutionStartPayload{}, false
}

func toolExecutionEndPayload(payload agent.NodeEventPayload) (agent.ToolExecutionEndPayload, bool) {
	switch value := payload.(type) {
	case agent.ToolExecutionEndPayload:
		return value, true
	case *agent.ToolExecutionEndPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.ToolExecutionEndPayload{}, false
}

func todoSnapshotPayload(payload agent.NodeEventPayload) (agent.TodoSnapshotPayload, bool) {
	switch value := payload.(type) {
	case agent.TodoSnapshotPayload:
		return value, true
	case *agent.TodoSnapshotPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.TodoSnapshotPayload{}, false
}

func todoUpdatedPayload(payload agent.NodeEventPayload) (agent.TodoUpdatedPayload, bool) {
	switch value := payload.(type) {
	case agent.TodoUpdatedPayload:
		return value, true
	case *agent.TodoUpdatedPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.TodoUpdatedPayload{}, false
}

func approvalRequestedPayload(payload agent.NodeEventPayload) (agent.ApprovalRequestedPayload, bool) {
	switch value := payload.(type) {
	case agent.ApprovalRequestedPayload:
		return value, true
	case *agent.ApprovalRequestedPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.ApprovalRequestedPayload{}, false
}

func approvalResolvedPayload(payload agent.NodeEventPayload) (agent.ApprovalResolvedPayload, bool) {
	switch value := payload.(type) {
	case agent.ApprovalResolvedPayload:
		return value, true
	case *agent.ApprovalResolvedPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.ApprovalResolvedPayload{}, false
}

func questionAskedPayload(payload agent.NodeEventPayload) (agent.QuestionAskedPayload, bool) {
	switch value := payload.(type) {
	case agent.QuestionAskedPayload:
		return value, true
	case *agent.QuestionAskedPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.QuestionAskedPayload{}, false
}

func questionAnsweredPayload(payload agent.NodeEventPayload) (agent.QuestionAnsweredPayload, bool) {
	switch value := payload.(type) {
	case agent.QuestionAnsweredPayload:
		return value, true
	case *agent.QuestionAnsweredPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.QuestionAnsweredPayload{}, false
}

func agentStartPayload(payload agent.NodeEventPayload) (agent.AgentStartedPayload, bool) {
	switch value := payload.(type) {
	case agent.AgentStartedPayload:
		return value, true
	case *agent.AgentStartedPayload:
		if value != nil {
			return *value, true
		}
	}
	return agent.AgentStartedPayload{}, false
}
