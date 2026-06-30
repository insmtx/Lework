// Package events 定义共享的运行时事件契约。
//
// 此包包含:
//   - 用于 API 和引擎接口的稳定运行时事件契约 (agent.Event, agent.EventType)
package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/agent"
)

// MarshalRaw encodes a dynamic provider value at the JSON protocol boundary.
func MarshalRaw(value any) json.RawMessage {
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

const (
	// EventStarted is the business run started event.
	EventStarted agent.EventType = "run.started"
	// EventCompleted is the business run completed event.
	EventCompleted agent.EventType = "run.completed"
	// EventFailed is the business run failed event.
	EventFailed agent.EventType = "run.failed"
	// EventCancelled is the business run cancelled event.
	EventCancelled agent.EventType = "run.cancelled"

	// EventInvocationStarted indicates a provider invocation has started.
	EventInvocationStarted agent.EventType = "provider.started"
	// EventInvocationCompleted indicates a provider invocation completed.
	EventInvocationCompleted agent.EventType = "provider.completed"
	// EventInvocationFailed indicates a provider invocation failed.
	EventInvocationFailed agent.EventType = "provider.failed"
	// EventInvocationCancelled indicates a provider invocation was cancelled.
	EventInvocationCancelled agent.EventType = "provider.cancelled"

	// EventMessageDelta 包含可读的助手输出。
	EventMessageDelta agent.EventType = "message.delta"
	// EventReasoningDelta 包含推理输出（如有）。
	EventReasoningDelta agent.EventType = "reasoning.delta"
	// EventResult 包含运行时运行的最终助手结果。
	EventResult agent.EventType = "message.result"
	// EventMessageComplete 表示消息已完成（包含完整内容）。
	EventMessageComplete agent.EventType = "message.complete"

	// EventToolCallStarted 表示工具调用已开始。
	EventToolCallStarted agent.EventType = "tool_call.started"
	// EventToolCallDelta 表示工具调用增量内容。
	EventToolCallDelta agent.EventType = "tool_call.delta"
	// EventToolCallCompleted 表示工具调用已完成。
	EventToolCallCompleted agent.EventType = "tool_call.completed"
	// EventToolCallResult 表示工具调用最终结果。
	EventToolCallResult agent.EventType = "tool_call.result"
	// EventToolCallFailed 表示工具调用失败。
	EventToolCallFailed agent.EventType = "tool_call.failed"

	// EventTodoSnapshot 包含当前运行的完整运行时待办列表。
	EventTodoSnapshot agent.EventType = "todo.snapshot"
	// EventTodoUpdated 包含更新后的完整运行时待办列表。
	EventTodoUpdated agent.EventType = "todo.updated"

	// EventArtifactDeclared indicates a generated artifact was declared by the runtime.
	EventArtifactDeclared agent.EventType = "artifact.declared"

	// EventApprovalRequested indicates the engine needs user approval for a tool call.
	EventApprovalRequested agent.EventType = "approval.requested"
	// EventApprovalResolved indicates an approval request has been resolved.
	EventApprovalResolved agent.EventType = "approval.resolved"

	// EventQuestionAsked indicates the engine is asking the user a clarifying question.
	EventQuestionAsked agent.EventType = "question.asked"
	// EventQuestionAnswered indicates a question has been answered by the user.
	EventQuestionAnswered agent.EventType = "question.answered"

	// EventWorkTitleUpdated indicates project/task titles were auto-generated after the first message.
	EventWorkTitleUpdated agent.EventType = "work.title.updated"

	// EventProviderSessionStarted indicates the provider exposed a native session ID.
	EventProviderSessionStarted agent.EventType = "provider_session.started"
)

// MessageDeltaPayload 是助手文本增量的标准负载。
type MessageDeltaPayload struct {
	MessageID string `json:"message_id,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content"`
}

// ToolCallPayload 是工具调用开始和参数事件的标准负载。
type ToolCallPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
}

// ToolCallResultPayload 是工具调用终止事件的标准负载。
type ToolCallResultPayload struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	IsError    bool            `json:"is_error"`
	ElapsedMS  int64           `json:"elapsed_ms,omitempty"`
}

// MessageResultPayload 描述最终助手结果和可选的使用情况元数据。
type MessageResultPayload struct {
	Message string       `json:"message,omitempty"`
	Usage   *agent.Usage `json:"usage,omitempty"`
}

// RuntimeTodoItem 描述一个运行时本地规划步骤。
type RuntimeTodoItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

// WorkTitleUpdatedPayload notifies clients that project/task titles were auto-generated.
type WorkTitleUpdatedPayload struct {
	ProjectID    string `json:"project_id"`
	ProjectName  string `json:"project_name"`
	TaskID       string `json:"task_id,omitempty"`
	TaskTitle    string `json:"task_title,omitempty"`
	SessionID    string `json:"session_id"`
	SessionTitle string `json:"session_title,omitempty"`
}

// RunEventRecord 是归一化、已归档的运行时事件。
type RunEventRecord struct {
	Seq       int64           `json:"seq,omitempty"`
	LastSeq   int64           `json:"last_seq,omitempty"`
	Type      agent.EventType `json:"type"`
	Timestamp int64           `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// ApprovalRequestPayload describes a tool call that needs user approval.
// 前端统一结构，不区分引擎类型。引擎特有信息放在 Metadata 中。
type ApprovalRequestPayload struct {
	RequestID   string            `json:"request_id"`
	ToolName    string            `json:"tool_name"`
	ToolCallID  string            `json:"tool_call_id,omitempty"`
	Description string            `json:"description"`
	Arguments   json.RawMessage   `json:"arguments,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ApprovalDecisionPayload describes the outcome of an approval request.
type ApprovalDecisionPayload struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"` // "approve" | "deny" | "always"
	Reason    string `json:"reason,omitempty"`
}

// QuestionRequestPayload describes a clarifying question from the engine to the user.
type QuestionRequestPayload struct {
	RequestID       string              `json:"request_id"`
	SessionID       string              `json:"session_id,omitempty"`
	Questions       []QuestionItem      `json:"questions"`
	ToolCallID      string              `json:"tool_call_id,omitempty"`
	MessageID       string              `json:"message_id,omitempty"`
	InteractionType string              `json:"interaction_type,omitempty"`
	Plan            *PlanHandoffPayload `json:"plan,omitempty"`
	Metadata        map[string]string   `json:"metadata,omitempty"`
}

// PlanHandoffPayload carries the plan content displayed during a plan confirmation.
type PlanHandoffPayload struct {
	Content  string `json:"content,omitempty"`
	FilePath string `json:"file_path,omitempty"`
	Error    string `json:"error,omitempty"`
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

// ArtifactPayload 引用单次运行产生的产物。
type ArtifactPayload struct {
	ArtifactID   string    `json:"artifact_id,omitempty"`
	Title        string    `json:"title,omitempty"`
	Filename     string    `json:"filename,omitempty"`
	OriginalName string    `json:"original_name,omitempty"`
	Description  string    `json:"description,omitempty"`
	MimeType     string    `json:"mime_type,omitempty"`
	ArtifactType string    `json:"artifact_type,omitempty"`
	FileSize     int64     `json:"file_size,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	RelativePath string    `json:"relative_path,omitempty"`
	StorageKey   string    `json:"storage_key,omitempty"`
	StorageURI   string    `json:"storage_uri,omitempty"`
	Sha256       string    `json:"sha256,omitempty"`
	Source       string    `json:"source,omitempty"`
	Status       string    `json:"status,omitempty"`
}

// NewMessageDelta 创建标准的助手消息增量事件。
func NewMessageDelta(messageID string, content string) *agent.Event {
	return newPayloadEvent(EventMessageDelta, MessageDeltaPayload{
		MessageID: strings.TrimSpace(messageID),
		Role:      "assistant",
		Content:   content,
	}, content)
}

// NewReasoningDelta 创建标准的推理增量事件。
func NewReasoningDelta(messageID string, content string) *agent.Event {
	return newPayloadEvent(EventReasoningDelta, MessageDeltaPayload{
		MessageID: strings.TrimSpace(messageID),
		Role:      "assistant",
		Content:   content,
	}, content)
}

// NewToolCallStarted 创建标准的工具调用开始事件。
func NewToolCallStarted(toolCallID string, name string, arguments json.RawMessage) *agent.Event {
	return newPayloadEvent(EventToolCallStarted, ToolCallPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Arguments:  arguments,
	}, "")
}

// NewToolCallCompleted 创建标准的成功工具调用终止事件。
func NewToolCallCompleted(toolCallID string, name string, result json.RawMessage, elapsedMS int64) *agent.Event {
	return newPayloadEvent(EventToolCallCompleted, ToolCallResultPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Result:     result,
		IsError:    false,
		ElapsedMS:  elapsedMS,
	}, "")
}

// NewToolCallFailed 创建标准的失败工具调用终止事件。
func NewToolCallFailed(toolCallID string, name string, message string, elapsedMS int64) *agent.Event {
	return newPayloadEvent(EventToolCallFailed, ToolCallResultPayload{
		ToolCallID: strings.TrimSpace(toolCallID),
		Name:       name,
		Error:      message,
		IsError:    true,
		ElapsedMS:  elapsedMS,
	}, "")
}

// NewArtifactDeclared 创建语义化的产物声明事件。
func NewArtifactDeclared(payload ArtifactPayload) *agent.Event {
	return newPayloadEvent(EventArtifactDeclared, payload, "")
}

// NewMessageResult 创建标准的最终助手结果事件。
func NewMessageResult(message string, usage *agent.Usage) *agent.Event {
	return newPayloadEvent(EventResult, MessageResultPayload{
		Message: message,
		Usage:   usage,
	}, message)
}

// NewTodoSnapshot 创建标准的完整待办快照事件。
func NewTodoSnapshot(items []RuntimeTodoItem) *agent.Event {
	return newPayloadEvent(EventTodoSnapshot, items, "")
}

// NewTodoUpdated 创建标准的完整待办更新事件。
func NewTodoUpdated(items []RuntimeTodoItem) *agent.Event {
	return newPayloadEvent(EventTodoUpdated, items, "")
}

// NewApprovalRequested creates an approval request event.
func NewApprovalRequested(payload ApprovalRequestPayload) *agent.Event {
	return newPayloadEvent(EventApprovalRequested, payload, payload.Description)
}

// NewApprovalResolved creates an approval resolution event.
func NewApprovalResolved(payload ApprovalDecisionPayload) *agent.Event {
	return newPayloadEvent(EventApprovalResolved, payload, payload.Action)
}

// NewQuestionAsked creates a question asked event.
func NewQuestionAsked(payload QuestionRequestPayload) *agent.Event {
	desc := "question"
	if len(payload.Questions) > 0 {
		desc = payload.Questions[0].Question
	}
	return newPayloadEvent(EventQuestionAsked, payload, desc)
}

// NewQuestionAnswered creates a question answered event.
func NewQuestionAnswered(payload QuestionAnswerPayload) *agent.Event {
	return newPayloadEvent(EventQuestionAnswered, payload, "")
}

// DecodePayload 从事件中解码结构化载荷。
func DecodePayload[T any](event *agent.Event) (T, error) {
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

func newPayloadEvent(eventType agent.EventType, payload any, contentFallback string) *agent.Event {
	raw, content := marshalPayload(payload)
	if contentFallback != "" {
		content = contentFallback
	}
	return &agent.Event{
		Type:    eventType,
		Payload: raw,
		Content: content,
	}
}

func marshalPayload(payload any) (json.RawMessage, string) {
	if payload == nil {
		return nil, ""
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Sprintf("%v", payload)
	}
	return json.RawMessage(raw), string(raw)
}
