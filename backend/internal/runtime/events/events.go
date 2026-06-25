// Package events 定义共享的运行时事件契约。
//
// 此包包含:
//   - 用于 API 和引擎接口的稳定运行时事件契约 (Event, EventType)
package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
)

// Event 是流式执行的稳定运行时事件信封。
type Event = agent.Event

// RawPayload 在保持 Event 易于序列化的同时存储结构化事件负载 JSON。
type RawPayload = agent.RawPayload

// UsagePayload 描述模型 token 的使用情况（如有）。
type UsagePayload = agent.Usage

// EventType 标识执行期间发出的一个可观测的运行时事件。
type EventType = agent.EventType

const (
	// EventStarted 表示运行时运行已开始。
	EventStarted EventType = "run.started"
	// EventCompleted 表示运行时运行已成功完成。
	EventCompleted EventType = "run.completed"
	// EventFailed 表示运行时运行失败。
	EventFailed EventType = "run.failed"
	// EventCancelled 表示运行时运行被取消。
	EventCancelled EventType = "run.cancelled"

	// EventMessageDelta 包含可读的助手输出。
	EventMessageDelta EventType = "message.delta"
	// EventReasoningDelta 包含推理输出（如有）。
	EventReasoningDelta EventType = "reasoning.delta"
	// EventResult 包含运行时运行的最终助手结果。
	EventResult EventType = "message.result"
	// EventMessageComplete 表示消息已完成（包含完整内容）。
	EventMessageComplete EventType = "message.complete"

	// EventToolCallStarted 表示工具调用已开始。
	EventToolCallStarted EventType = "tool_call.started"
	// EventToolCallDelta 表示工具调用增量内容。
	EventToolCallDelta EventType = "tool_call.delta"
	// EventToolCallCompleted 表示工具调用已完成。
	EventToolCallCompleted EventType = "tool_call.completed"
	// EventToolCallResult 表示工具调用最终结果。
	EventToolCallResult EventType = "tool_call.result"
	// EventToolCallFailed 表示工具调用失败。
	EventToolCallFailed EventType = "tool_call.failed"

	// EventTodoSnapshot 包含当前运行的完整运行时待办列表。
	EventTodoSnapshot EventType = "todo.snapshot"
	// EventTodoUpdated 包含更新后的完整运行时待办列表。
	EventTodoUpdated EventType = "todo.updated"

	// EventArtifactDeclared indicates a generated artifact was declared by the runtime.
	EventArtifactDeclared EventType = "artifact.declared"

	// EventApprovalRequested indicates the engine needs user approval for a tool call.
	EventApprovalRequested EventType = "approval.requested"
	// EventApprovalResolved indicates an approval request has been resolved.
	EventApprovalResolved EventType = "approval.resolved"

	// EventQuestionAsked indicates the engine is asking the user a clarifying question.
	EventQuestionAsked EventType = "question.asked"
	// EventQuestionAnswered indicates a question has been answered by the user.
	EventQuestionAnswered EventType = "question.answered"
)

// MessageDeltaPayload 是助手文本增量的标准负载。
type MessageDeltaPayload struct {
	MessageID string `json:"message_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content"`
}

// ToolCallPayload 是工具调用开始和参数事件的标准负载。
type ToolCallPayload struct {
	ToolCallID string         `json:"tool_call_id"`
	Name       string         `json:"name"`
	Arguments  map[string]any `json:"arguments,omitempty"`
}

// ToolCallResultPayload 是工具调用终止事件的标准负载。
type ToolCallResultPayload struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name,omitempty"`
	Result     any    `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	IsError    bool   `json:"is_error"`
	ElapsedMS  int64  `json:"elapsed_ms,omitempty"`
}

// MessageResultPayload 描述最终助手结果和可选的使用情况元数据。
type MessageResultPayload struct {
	Message string        `json:"message,omitempty"`
	Usage   *UsagePayload `json:"usage,omitempty"`
}

// RunResultPayload 描述归档在 run.completed 中的最终助手结果。
type RunResultPayload struct {
	Message string `json:"message,omitempty"`
}

// RuntimeTodoItem 描述一个运行时本地规划步骤。
type RuntimeTodoItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

// RunEventRecord 是归一化、已归档的运行时事件。
type RunEventRecord struct {
	Seq       int64      `json:"seq,omitempty"`
	LastSeq   int64      `json:"last_seq,omitempty"`
	Type      EventType  `json:"type"`
	Timestamp int64      `json:"timestamp,omitempty"`
	Payload   RawPayload `json:"payload,omitempty"`
}

// ApprovalRequestPayload describes a tool call that needs user approval.
// 前端统一结构，不区分引擎类型。引擎特有信息放在 Metadata 中。
type ApprovalRequestPayload struct {
	RequestID   string         `json:"request_id"`
	ToolName    string         `json:"tool_name"`
	ToolCallID  string         `json:"tool_call_id,omitempty"`
	Description string         `json:"description"`
	Arguments   map[string]any `json:"arguments,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ApprovalDecisionPayload describes the outcome of an approval request.
type ApprovalDecisionPayload struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"` // "approve" | "deny" | "always"
	Reason    string `json:"reason,omitempty"`
}

// QuestionRequestPayload describes a clarifying question from the engine to the user.
type QuestionRequestPayload struct {
	RequestID  string         `json:"request_id"`
	SessionID  string         `json:"session_id,omitempty"`
	Questions  []QuestionItem `json:"questions"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	MessageID  string         `json:"message_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// QuestionItem is a single question within a question request.
type QuestionItem struct {
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multiple"`
	Custom      bool             `json:"custom"`
}

// QuestionOption is a choice option for a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionAnswerPayload describes the user's answer to a question request.
type QuestionAnswerPayload struct {
	RequestID string     `json:"request_id"`
	Answers   [][]string `json:"answers"`
}

// RunCompletedPayload 归档完整的成功运行时运行。
type RunCompletedPayload struct {
	Status      string            `json:"status"`
	Result      RunResultPayload  `json:"result"`
	Artifacts   []ArtifactPayload `json:"artifacts,omitempty"`
	Usage       *UsagePayload     `json:"usage,omitempty"`
	Events      []RunEventRecord  `json:"events,omitempty"`
	StartedAt   time.Time         `json:"started_at,omitempty"`
	CompletedAt time.Time         `json:"completed_at,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

// ArtifactPayload 引用单次运行产生的产物。
type ArtifactPayload struct {
	ArtifactID   string    `json:"artifact_id,omitempty"`
	Title        string    `json:"title,omitempty"`
	Filename     string    `json:"filename,omitempty"`
	Description  string    `json:"description,omitempty"`
	MimeType     string    `json:"mime_type,omitempty"`
	ArtifactType string    `json:"artifact_type,omitempty"`
	FileSize     int64     `json:"file_size,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	RelativePath string    `json:"relative_path,omitempty"`
	StorageKey   string    `json:"storage_key,omitempty"`
	Sha256       string    `json:"sha256,omitempty"`
	Source       string    `json:"source,omitempty"`
	Status       string    `json:"status,omitempty"`
}

// NewMessageDelta 创建标准的助手消息增量事件。
func NewMessageDelta(messageID string, content string) *Event {
	return newPayloadEvent(EventMessageDelta, MessageDeltaPayload{
		MessageID: strings.TrimSpace(messageID),
		Role:      "assistant",
		Content:   content,
	}, content)
}

// NewReasoningDelta 创建标准的推理增量事件。
func NewReasoningDelta(messageID string, content string) *Event {
	return newPayloadEvent(EventReasoningDelta, MessageDeltaPayload{
		MessageID: strings.TrimSpace(messageID),
		Role:      "assistant",
		Content:   content,
	}, content)
}

// NewToolCallStarted 创建标准的工具调用开始事件。
func NewToolCallStarted(toolCallID string, name string, arguments map[string]any) *Event {
	return newPayloadEvent(EventToolCallStarted, ToolCallPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Arguments:  arguments,
	}, "")
}

// NewToolCallCompleted 创建标准的成功工具调用终止事件。
func NewToolCallCompleted(toolCallID string, name string, result any, elapsedMS int64) *Event {
	return newPayloadEvent(EventToolCallCompleted, ToolCallResultPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Result:     result,
		IsError:    false,
		ElapsedMS:  elapsedMS,
	}, "")
}

// NewToolCallFailed 创建标准的失败工具调用终止事件。
func NewToolCallFailed(toolCallID string, name string, message string, elapsedMS int64) *Event {
	return newPayloadEvent(EventToolCallFailed, ToolCallResultPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Error:      message,
		IsError:    true,
		ElapsedMS:  elapsedMS,
	}, "")
}

// NewArtifactDeclared 创建语义化的产物声明事件。
func NewArtifactDeclared(payload ArtifactPayload) *Event {
	return newPayloadEvent(EventArtifactDeclared, payload, "")
}

// NewMessageResult 创建标准的最终助手结果事件。
func NewMessageResult(message string, usage *UsagePayload) *Event {
	return newPayloadEvent(EventResult, MessageResultPayload{
		Message: message,
		Usage:   usage,
	}, message)
}

// NewRunCompleted 创建标准的已完成运行归档事件。
func NewRunCompleted(payload RunCompletedPayload, contentFallback string) *Event {
	return newPayloadEvent(EventCompleted, payload, contentFallback)
}

// NewTodoSnapshot 创建标准的完整待办快照事件。
func NewTodoSnapshot(items []RuntimeTodoItem) *Event {
	return newPayloadEvent(EventTodoSnapshot, items, "")
}

// NewTodoUpdated 创建标准的完整待办更新事件。
func NewTodoUpdated(items []RuntimeTodoItem) *Event {
	return newPayloadEvent(EventTodoUpdated, items, "")
}

// NewApprovalRequested creates an approval request event.
func NewApprovalRequested(payload ApprovalRequestPayload) *Event {
	return newPayloadEvent(EventApprovalRequested, payload, payload.Description)
}

// NewApprovalResolved creates an approval resolution event.
func NewApprovalResolved(payload ApprovalDecisionPayload) *Event {
	return newPayloadEvent(EventApprovalResolved, payload, payload.Action)
}

// NewQuestionAsked creates a question asked event.
func NewQuestionAsked(payload QuestionRequestPayload) *Event {
	desc := "question"
	if len(payload.Questions) > 0 {
		desc = payload.Questions[0].Question
	}
	return newPayloadEvent(EventQuestionAsked, payload, desc)
}

// NewQuestionAnswered creates a question answered event.
func NewQuestionAnswered(payload QuestionAnswerPayload) *Event {
	return newPayloadEvent(EventQuestionAnswered, payload, "")
}

// DecodePayload 从事件中解码结构化载荷。
func DecodePayload[T any](event *Event) (T, error) {
	var zero T
	if event == nil {
		return zero, fmt.Errorf("event is nil")
	}
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &zero); err != nil {
			return zero, err
		}
		return zero, nil
	}
	if strings.TrimSpace(event.Content) == "" {
		return zero, fmt.Errorf("event payload is empty")
	}
	if err := json.Unmarshal([]byte(event.Content), &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

func newPayloadEvent(eventType EventType, payload any, contentFallback string) *Event {
	raw, content := marshalPayload(payload)
	if contentFallback != "" {
		content = contentFallback
	}
	return &Event{
		Type:    eventType,
		Payload: raw,
		Content: content,
	}
}

func marshalPayload(payload any) (RawPayload, string) {
	if payload == nil {
		return nil, ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Sprintf("%v", payload)
	}
	return RawPayload(raw), string(raw)
}
