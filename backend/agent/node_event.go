package agent

import (
	"context"
	"encoding/json"
	"time"
)

// NodeEventType identifies an observable runtime node event emitted during execution.
type NodeEventType string

// Runtime activity event types emitted by Runtime adapters.
// Adapters map native provider events into these canonical types.
const (
	NodeEventAgentStart NodeEventType = "agent.start"
	NodeEventAgentEnd   NodeEventType = "agent.end"

	NodeEventMessageStart    NodeEventType = "message.start"
	NodeEventMessageUpdate   NodeEventType = "message.update"
	NodeEventReasoningUpdate NodeEventType = "reasoning.update"
	NodeEventMessageEnd      NodeEventType = "message.end"

	NodeEventToolExecutionStart  NodeEventType = "tool_execution.start"
	NodeEventToolExecutionUpdate NodeEventType = "tool_execution.update"
	NodeEventToolExecutionEnd    NodeEventType = "tool_execution.end"

	NodeEventTodoSnapshot NodeEventType = "todo.snapshot"
	NodeEventTodoUpdated  NodeEventType = "todo.updated"

	NodeEventApprovalRequested NodeEventType = "approval.requested"
	NodeEventApprovalResolved  NodeEventType = "approval.resolved"
	NodeEventQuestionAsked     NodeEventType = "question.asked"
	NodeEventQuestionAnswered  NodeEventType = "question.answered"

	NodeEventPlanReady NodeEventType = "plan.ready"
)

// NodeEvent is the stable runtime node event envelope emitted during execution.
// It describes execution facts, not business state.
type NodeEvent struct {
	ID          string            `json:"id"`
	ExecutionID string            `json:"execution_id"`
	TraceID     string            `json:"trace_id"`
	Type        NodeEventType     `json:"type"`
	OccurredAt  time.Time         `json:"occurred_at"`
	Payload     NodeEventPayload  `json:"payload,omitempty"`
	Metadata    NodeEventMetadata `json:"metadata,omitempty"`
}

// NodeEventMetadata carries typed debug fields. It MUST NOT contain
// API Key, Authorization Header, or raw environment variables.
type NodeEventMetadata map[string]string

// NodeEventPayload is a sealed set of strongly-typed event payloads.
// Each concrete payload implements the marker method nodeEventPayload().
type NodeEventPayload interface {
	nodeEventPayload()
}

// ---- Payloads ----

// MessageStartPayload carries the start of a message.
type MessageStartPayload struct {
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
}

func (MessageStartPayload) nodeEventPayload() {}

// MessageUpdatePayload carries a streaming text delta.
type MessageUpdatePayload struct {
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
}

func (MessageUpdatePayload) nodeEventPayload() {}

// MessageEndPayload carries the final assembled message.
type MessageEndPayload struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
	Usage     *Usage `json:"usage,omitempty"`
}

func (MessageEndPayload) nodeEventPayload() {}

// ReasoningUpdatePayload carries a reasoning/thinking text update.
type ReasoningUpdatePayload struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
}

func (ReasoningUpdatePayload) nodeEventPayload() {}

// ToolExecutionStartPayload carries the start of a tool execution.
type ToolExecutionStartPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
}

func (ToolExecutionStartPayload) nodeEventPayload() {}

// ToolExecutionUpdatePayload carries incremental tool execution content.
type ToolExecutionUpdatePayload struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

func (ToolExecutionUpdatePayload) nodeEventPayload() {}

// ToolExecutionEndPayload carries the result of a tool execution (success or failure).
type ToolExecutionEndPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	IsError    bool            `json:"is_error"`
	Error      string          `json:"error,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	ElapsedMS  int64           `json:"elapsed_ms,omitempty"`
}

func (ToolExecutionEndPayload) nodeEventPayload() {}

// RuntimeTodoItem describes a single runtime planning step.
type RuntimeTodoItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

// TodoSnapshotPayload carries a complete todo list snapshot.
type TodoSnapshotPayload struct {
	Items []RuntimeTodoItem `json:"items"`
}

func (TodoSnapshotPayload) nodeEventPayload() {}

// TodoUpdatedPayload carries an updated complete todo list.
type TodoUpdatedPayload struct {
	Items []RuntimeTodoItem `json:"items"`
}

func (TodoUpdatedPayload) nodeEventPayload() {}

// ApprovalRequestedPayload describes a tool call that needs user approval.
type ApprovalRequestedPayload struct {
	RequestID   string            `json:"request_id"`
	ToolName    string            `json:"tool_name"`
	ToolCallID  string            `json:"tool_call_id"`
	Description string            `json:"description"`
	Arguments   json.RawMessage   `json:"arguments,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func (ApprovalRequestedPayload) nodeEventPayload() {}

// ApprovalResolvedPayload describes the outcome of an approval request.
type ApprovalResolvedPayload struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"` // "approve" | "deny" | "always"
	Reason    string `json:"reason,omitempty"`
}

func (ApprovalResolvedPayload) nodeEventPayload() {}

// QuestionAskedPayload describes a clarifying question from the runtime.
type QuestionAskedPayload struct {
	RequestID       string            `json:"request_id"`
	SessionID       string            `json:"session_id"`
	Questions       []QuestionItem    `json:"questions"`
	ToolCallID      string            `json:"tool_call_id"`
	MessageID       string            `json:"message_id"`
	InteractionType string            `json:"interaction_type,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

func (QuestionAskedPayload) nodeEventPayload() {}

// QuestionAnsweredPayload describes the user's answer to a question request.
type QuestionAnsweredPayload struct {
	RequestID string     `json:"request_id"`
	Answers   [][]string `json:"answers"`
}

func (QuestionAnsweredPayload) nodeEventPayload() {}

// AgentStartedPayload signals that the agent started and exposed a native provider session ID.
type AgentStartedPayload struct {
	ProviderSessionID string `json:"provider_session_id"`
}

func (AgentStartedPayload) nodeEventPayload() {}

// AgentEndedPayload signals that the agent execution has ended.
type AgentEndedPayload struct {
	ProviderSessionID string `json:"provider_session_id,omitempty"`
}

func (AgentEndedPayload) nodeEventPayload() {}

// PlanReadyPayload carries the safe path information for a detected plan file.
// Runtime emits only path info; Worker handles content reading, validation, and upload.
type PlanReadyPayload struct {
	Path              string `json:"path"`
	DisplayPath       string `json:"display_path"`
	ProviderSessionID string `json:"provider_session_id,omitempty"`
}

func (PlanReadyPayload) nodeEventPayload() {}

// ---- NodeObserver ----

// NodeObserver receives node events emitted during runtime execution.
// An observer that returns an error terminates the execution.
type NodeObserver interface {
	Observe(ctx context.Context, event NodeEvent) error
}

// NodeObserverFunc adapts a function to the NodeObserver interface.
type NodeObserverFunc func(ctx context.Context, event NodeEvent) error

func (f NodeObserverFunc) Observe(ctx context.Context, event NodeEvent) error {
	if f == nil {
		return nil
	}
	return f(ctx, event)
}

var _ NodeObserver = NodeObserverFunc(nil)

// ---- NodeEvent constructors for use by runtime adapters ----

// NewMessageStartEvent creates a message.start node event.
func NewMessageStartEvent(messageID, role string) NodeEvent {
	return NodeEvent{
		Type: NodeEventMessageStart,
		Payload: &MessageStartPayload{
			MessageID: messageID,
			Role:      role,
		},
	}
}

// NewMessageUpdateEvent creates a message.update node event.
func NewMessageUpdateEvent(messageID, content string) NodeEvent {
	return NodeEvent{
		Type: NodeEventMessageUpdate,
		Payload: &MessageUpdatePayload{
			MessageID: messageID,
			Role:      "assistant",
			Content:   content,
		},
	}
}

// NewReasoningUpdateEvent creates a reasoning.update node event.
func NewReasoningUpdateEvent(messageID, content string) NodeEvent {
	return NodeEvent{
		Type: NodeEventReasoningUpdate,
		Payload: &ReasoningUpdatePayload{
			MessageID: messageID,
			Content:   content,
		},
	}
}

// NewToolExecutionStartEvent creates a tool_execution.start node event.
func NewToolExecutionStartEvent(toolCallID, name string, arguments json.RawMessage) NodeEvent {
	return NodeEvent{
		Type: NodeEventToolExecutionStart,
		Payload: &ToolExecutionStartPayload{
			ToolCallID: toolCallID,
			Name:       name,
			Arguments:  arguments,
		},
	}
}

// NewToolExecutionUpdateEvent creates a tool_execution.update node event.
func NewToolExecutionUpdateEvent(toolCallID, content string) NodeEvent {
	return NodeEvent{
		Type: NodeEventToolExecutionUpdate,
		Payload: &ToolExecutionUpdatePayload{
			ToolCallID: toolCallID,
			Content:    content,
		},
	}
}

// NewToolExecutionEndEvent creates a tool_execution.end node event for a successful tool call.
// For failed tool calls, use NewToolExecutionEndErrorEvent.
func NewToolExecutionEndEvent(toolCallID, name string, result json.RawMessage, elapsedMS int64) NodeEvent {
	return NodeEvent{
		Type: NodeEventToolExecutionEnd,
		Payload: &ToolExecutionEndPayload{
			ToolCallID: toolCallID,
			Name:       name,
			IsError:    false,
			Result:     result,
			ElapsedMS:  elapsedMS,
		},
	}
}

// NewToolExecutionEndErrorEvent creates a tool_execution.end node event for a failed tool call.
func NewToolExecutionEndErrorEvent(toolCallID, name, detail string, elapsedMS int64) NodeEvent {
	return NodeEvent{
		Type: NodeEventToolExecutionEnd,
		Payload: &ToolExecutionEndPayload{
			ToolCallID: toolCallID,
			Name:       name,
			IsError:    true,
			Error:      detail,
			ElapsedMS:  elapsedMS,
		},
	}
}

// NewTodoSnapshotEvent creates a todo.snapshot node event.
func NewTodoSnapshotEvent(items []RuntimeTodoItem) NodeEvent {
	return NodeEvent{
		Type:    NodeEventTodoSnapshot,
		Payload: &TodoSnapshotPayload{Items: items},
	}
}

// NewTodoUpdatedEvent creates a todo.updated node event.
func NewTodoUpdatedEvent(items []RuntimeTodoItem) NodeEvent {
	return NodeEvent{
		Type:    NodeEventTodoUpdated,
		Payload: &TodoUpdatedPayload{Items: items},
	}
}

// NewMessageEndEvent creates a message.end node event.
func NewMessageEndEvent(content string, usage *Usage) NodeEvent {
	return NodeEvent{
		Type: NodeEventMessageEnd,
		Payload: &MessageEndPayload{
			Content: content,
			Usage:   EnsureUsage(usage),
		},
	}
}

// NewApprovalRequestedEvent creates an approval.requested node event.
func NewApprovalRequestedEvent(p ApprovalRequestedPayload) NodeEvent {
	return NodeEvent{
		Type:    NodeEventApprovalRequested,
		Payload: &p,
	}
}

// NewApprovalResolvedEvent creates an approval.resolved node event.
func NewApprovalResolvedEvent(p ApprovalResolvedPayload) NodeEvent {
	return NodeEvent{
		Type:    NodeEventApprovalResolved,
		Payload: &p,
	}
}

// NewQuestionAskedEvent creates a question.asked node event.
func NewQuestionAskedEvent(p QuestionAskedPayload) NodeEvent {
	return NodeEvent{
		Type:    NodeEventQuestionAsked,
		Payload: &p,
	}
}

// NewQuestionAnsweredEvent creates a question.answered node event.
func NewQuestionAnsweredEvent(p QuestionAnsweredPayload) NodeEvent {
	return NodeEvent{
		Type:    NodeEventQuestionAnswered,
		Payload: &p,
	}
}

// NewAgentStartEvent creates an agent.start node event.
func NewAgentStartEvent(sessionID string) NodeEvent {
	return NodeEvent{
		Type: NodeEventAgentStart,
		Payload: &AgentStartedPayload{
			ProviderSessionID: sessionID,
		},
	}
}

// NewAgentEndEvent creates an agent.end node event.
func NewAgentEndEvent(sessionID string) NodeEvent {
	return NodeEvent{
		Type: NodeEventAgentEnd,
		Payload: &AgentEndedPayload{
			ProviderSessionID: sessionID,
		},
	}
}

// NewPlanReadyEvent creates a plan.ready node event.
func NewPlanReadyEvent(path, displayPath, sessionID string) NodeEvent {
	return NodeEvent{
		Type: NodeEventPlanReady,
		Payload: &PlanReadyPayload{
			Path:              path,
			DisplayPath:       displayPath,
			ProviderSessionID: sessionID,
		},
	}
}

// MarshalRawJSON encodes an arbitrary value to json.RawMessage.
func MarshalRawJSON(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	switch raw := value.(type) {
	case json.RawMessage:
		return append(json.RawMessage(nil), raw...)
	case []byte:
		return append(json.RawMessage(nil), raw...)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}
