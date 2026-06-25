package protocol

import "github.com/insmtx/Leros/backend/internal/runtime/events"

// StreamEventType represents event types in Worker execution streams.
type StreamEventType string

const (
	// StreamEventRunStarted indicates a run has started.
	StreamEventRunStarted StreamEventType = "run.started"
	// StreamEventRunCompleted indicates a run completed successfully.
	StreamEventRunCompleted StreamEventType = "run.completed"
	// StreamEventRunFailed indicates a run failed.
	StreamEventRunFailed StreamEventType = "run.failed"

	// StreamEventMessageDelta indicates incremental text output from assistant.
	StreamEventMessageDelta StreamEventType = "message.delta"
	// StreamEventReasoningDelta indicates incremental reasoning output from assistant.
	StreamEventReasoningDelta StreamEventType = "reasoning.delta"

	// StreamEventMessageCompleted indicates the final assistant message is generated.
	StreamEventMessageCompleted StreamEventType = "message.completed"

	// StreamEventToolCallStarted indicates a tool call has started.
	StreamEventToolCallStarted StreamEventType = "tool_call.started"
	// StreamEventToolCallFinished indicates a tool call has finished.
	StreamEventToolCallFinished StreamEventType = "tool_call.finished"

	// StreamEventTodoSnapshot indicates the full runtime todo list is available.
	StreamEventTodoSnapshot StreamEventType = "todo.snapshot"
	// StreamEventTodoUpdated indicates the runtime todo list changed.
	StreamEventTodoUpdated StreamEventType = "todo.updated"
	// StreamEventArtifactDeclared indicates a generated artifact was declared.
	StreamEventArtifactDeclared StreamEventType = "artifact.declared"

	// StreamEventApprovalRequested indicates the engine needs user approval for a tool call.
	StreamEventApprovalRequested StreamEventType = "approval.requested"
	// StreamEventApprovalResolved indicates an approval request has been resolved.
	StreamEventApprovalResolved StreamEventType = "approval.resolved"

	// StreamEventQuestionAsked indicates the engine is asking the user a question.
	StreamEventQuestionAsked StreamEventType = "question.asked"
	// StreamEventQuestionAnswered indicates the question has been answered.
	StreamEventQuestionAnswered StreamEventType = "question.answered"
)

// MessageStreamMessage is the stream message protocol from Worker to Server (forwarded to UI).
type MessageStreamMessage = Envelope[StreamBody]

// StreamBody is a single streaming event payload from Worker to Server to UI.
type StreamBody struct {
	Seq               int64           `json:"seq"`
	Event             StreamEventType `json:"event"`
	Payload           StreamPayload   `json:"payload"`
	ReplyToMessageIDs []string        `json:"reply_to_message_ids,omitempty"`

	RunCompleted *events.RunCompletedPayload `json:"run_completed,omitempty"`
	Error        *StreamError                `json:"error,omitempty"`
}

// StreamPayload carries the specific content of streaming events.
type StreamPayload struct {
	MessageID        string                          `json:"message_id,omitempty"`
	Role             MessageRole                     `json:"role,omitempty"`
	Content          string                          `json:"content,omitempty"`
	Usage            *events.UsagePayload            `json:"usage,omitempty"`
	ToolCall         *events.ToolCallPayload         `json:"tool_call,omitempty"`
	ToolResult       *events.ToolCallResultPayload   `json:"tool_result,omitempty"`
	Todos            []events.RuntimeTodoItem        `json:"todos,omitempty"`
	Artifact         *events.ArtifactPayload         `json:"artifact,omitempty"`
	ApprovalRequest  *events.ApprovalRequestPayload  `json:"approval_request,omitempty"`
	ApprovalDecision *events.ApprovalDecisionPayload `json:"approval_decision,omitempty"`
	QuestionRequest  *events.QuestionRequestPayload  `json:"question_request,omitempty"`
	QuestionAnswer   *events.QuestionAnswerPayload   `json:"question_answer,omitempty"`
}

// StreamError describes terminal or recoverable errors in streaming execution.
type StreamError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}
