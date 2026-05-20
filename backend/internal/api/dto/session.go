package dto

type SessionEventType string

const (
	SessionEventTypeMessageDelta    SessionEventType = "message.delta"
	SessionEventTypeReasoningDelta  SessionEventType = "reasoning.delta"
	SessionEventTypeMessageComplete SessionEventType = "message.complete"
	SessionEventTypeRunStarted      SessionEventType = "run.started"
	SessionEventTypeRunCompleted    SessionEventType = "run.completed"
	SessionEventTypeRunFailed       SessionEventType = "run.failed"
	SessionEventTypeToolCallStarted SessionEventType = "tool_call.started"
	SessionEventTypeToolCallDelta   SessionEventType = "tool_call.delta"
	SessionEventTypeToolCallResult  SessionEventType = "tool_call.result"
)

type SessionEvent struct {
	Type      SessionEventType `json:"type"`
	SessionID string           `json:"session_id"`
	Payload   interface{}      `json:"payload"`
	Sequence  int64            `json:"sequence"`
	Timestamp int64            `json:"timestamp"` // Unix timestamp in milliseconds
}

type MessageDeltaPayload struct {
	MessageID string `json:"message_id,omitempty"`
	Role      string `json:"role"`
	Content   string `json:"content"` // 增量文本
}

type RunStatusPayload struct {
	Status  string `json:"status"`
	RunID   string `json:"run_id,omitempty"`
	Message string `json:"message,omitempty"`
}

type ToolCallDeltaPayload struct {
	ToolCallID string                 `json:"tool_call_id"`
	Name       string                 `json:"name,omitempty"`
	Arguments  map[string]interface{} `json:"arguments,omitempty"`
}

type ToolCallResultPayload struct {
	ToolCallID string      `json:"tool_call_id"`
	Name       string      `json:"name"`
	Result     interface{} `json:"result"`
	Status     string      `json:"status"` // success | error
}
