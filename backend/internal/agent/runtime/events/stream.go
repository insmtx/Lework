package events

// StreamEventType represents event types in Worker execution streams.
type StreamEventType string

const (
	// StreamEventRunStarted indicates a run has started.
	StreamEventRunStarted StreamEventType = "run.started"
	// StreamEventMessageDelta indicates incremental text output from assistant.
	StreamEventMessageDelta StreamEventType = "message.delta"
	// StreamEventReasoningDelta indicates incremental reasoning output from assistant.
	StreamEventReasoningDelta StreamEventType = "reasoning.delta"
	// StreamEventToolCallStarted indicates a tool call has started.
	StreamEventToolCallStarted StreamEventType = "tool_call.started"
	// StreamEventToolCallFinished indicates a tool call has finished.
	StreamEventToolCallFinished StreamEventType = "tool_call.finished"
	// StreamEventMessageCompleted indicates the final assistant message is generated.
	StreamEventMessageCompleted StreamEventType = "message.completed"
	// StreamEventRunCompleted indicates a run completed successfully.
	StreamEventRunCompleted StreamEventType = "run.completed"
	// StreamEventRunFailed indicates a run failed.
	StreamEventRunFailed StreamEventType = "run.failed"
)

// MessageStreamMessage is the stream message protocol from Worker to Server (forwarded to UI).
type MessageStreamMessage = Envelope[StreamBody]

// StreamBody is a single streaming event payload from Worker to Server to UI.
type StreamBody struct {
	Seq     int64           `json:"seq"`
	Event   StreamEventType `json:"event"`
	Payload StreamPayload   `json:"payload"`

	RunCompleted *RunCompletedPayload `json:"run_completed,omitempty"`
	Error        *StreamError         `json:"error,omitempty"`
}

// StreamPayload carries the specific content of streaming events.
type StreamPayload struct {
	MessageID  string                 `json:"message_id,omitempty"`
	Role       MessageRole            `json:"role,omitempty"`
	Content    string                 `json:"content,omitempty"`
	Usage      *UsagePayload          `json:"usage,omitempty"`
	ToolCall   *ToolCallPayload       `json:"tool_call,omitempty"`
	ToolResult *ToolCallResultPayload `json:"tool_result,omitempty"`
}

// StreamError describes terminal or recoverable errors in streaming execution.
type StreamError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}
