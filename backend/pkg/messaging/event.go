package messaging

import "encoding/json"

// RunEventType 表示 worker 运行事件的类型。
type RunEventType string

const (
	// ---- state lane 事件（低频关键状态） ----

	// RunEventRunStarted 表示 run 已开始。
	RunEventRunStarted RunEventType = "run.started"
	// RunEventRunCompleted 表示 run 成功完成。
	RunEventRunCompleted RunEventType = "run.completed"
	// RunEventRunFailed 表示 run 失败。
	RunEventRunFailed RunEventType = "run.failed"
	// RunEventRunCancelled 表示 run 被取消。
	RunEventRunCancelled RunEventType = "run.cancelled"

	// RunEventArtifactDeclared 表示生成了产物。
	RunEventArtifactDeclared RunEventType = "artifact.declared"

	// RunEventApprovalRequested 表示引擎需要用户审批。
	RunEventApprovalRequested RunEventType = "approval.requested"
	// RunEventApprovalResolved 表示审批已被解决。
	RunEventApprovalResolved RunEventType = "approval.resolved"

	// RunEventQuestionAsked 表示引擎正在向用户提问。
	RunEventQuestionAsked RunEventType = "question.asked"
	// RunEventQuestionAnswered 表示问题已被回答。
	RunEventQuestionAnswered RunEventType = "question.answered"

	// RunEventWorkTitleUpdated 表示项目/任务标题已由 LLM 自动生成。
	RunEventWorkTitleUpdated RunEventType = "work.title.updated"

	// ---- stream lane 事件（高频增量，SSE 实时流） ----

	// RunEventMessageDelta 表示助手文本增量输出。
	RunEventMessageDelta RunEventType = "message.delta"
	// RunEventReasoningDelta 表示推理文本增量输出。
	RunEventReasoningDelta RunEventType = "reasoning.delta"

	// RunEventMessageCompleted 表示最终助手消息已生成。
	RunEventMessageCompleted RunEventType = "message.completed"

	// RunEventToolCallStarted 表示工具调用开始。
	RunEventToolCallStarted RunEventType = "tool_call.started"
	// RunEventToolCallFinished 表示工具调用结束。
	RunEventToolCallFinished RunEventType = "tool_call.finished"

	// RunEventTodoSnapshot 表示完整运行时 todo 列表可用。
	RunEventTodoSnapshot RunEventType = "todo.snapshot"
	// RunEventTodoUpdated 表示运行时 todo 列表已更新。
	RunEventTodoUpdated RunEventType = "todo.updated"
)

// RunEventLane 表示 run event 发送到哪个 lane。
type RunEventLane string

const (
	// RunEventLaneStream 是高频 SSE 增量 lane。
	RunEventLaneStream RunEventLane = "run.stream"
	// RunEventLaneState 是低频关键状态 lane。
	RunEventLaneState RunEventLane = "run.state"
)

// ClassifyRunEvent 根据事件类型返回对应的 lane。
// 高频增量事件进入 stream lane，关键状态和终端事件进入 state lane。
func ClassifyRunEvent(eventType RunEventType) RunEventLane {
	switch eventType {
	// State lane — 低频关键状态
	case RunEventRunStarted, RunEventRunCompleted, RunEventRunFailed, RunEventRunCancelled,
		RunEventArtifactDeclared,
		RunEventApprovalRequested, RunEventApprovalResolved,
		RunEventQuestionAsked, RunEventQuestionAnswered,
		RunEventWorkTitleUpdated:
		return RunEventLaneState

	// Stream lane — 高频增量
	case RunEventMessageDelta, RunEventReasoningDelta,
		RunEventMessageCompleted,
		RunEventToolCallStarted, RunEventToolCallFinished,
		RunEventTodoSnapshot, RunEventTodoUpdated:
		return RunEventLaneStream

	default:
		return RunEventLaneStream
	}
}

// RunEvent 是 Worker -> Server/UI 的统一运行事件消息。
type RunEvent = Envelope[RunEventBody]

// RunEventBody 是单个运行事件的 payload。
type RunEventBody struct {
	Seq               int64           `json:"seq"`
	Event             RunEventType    `json:"event"`
	Payload           RunEventPayload `json:"payload"`
	ReplyToMessageIDs []string        `json:"reply_to_message_ids,omitempty"`

	// RunCompleted 仅在终端事件（run.completed/failed/cancelled）时填充。
	RunCompleted *RunCompletedPayload `json:"run_completed,omitempty"`
	// Error 仅在 run.failed 时填充。
	Error *RunEventError `json:"error,omitempty"`
}

// RunEventPayload 携带流事件的特定内容。
type RunEventPayload struct {
	MessageID        string                   `json:"message_id,omitempty"`
	Role             MessageRole              `json:"role,omitempty"`
	Content          string                   `json:"content,omitempty"`
	Usage            *UsagePayload            `json:"usage,omitempty"`
	ToolCall         *ToolCallPayload         `json:"tool_call,omitempty"`
	ToolResult       *ToolCallResultPayload   `json:"tool_result,omitempty"`
	Todos            []RuntimeTodoItem        `json:"todos,omitempty"`
	Artifact         *ArtifactPayload         `json:"artifact,omitempty"`
	ApprovalRequest  *ApprovalRequestPayload  `json:"approval_request,omitempty"`
	ApprovalDecision *ApprovalDecisionPayload `json:"approval_decision,omitempty"`
	QuestionRequest  *QuestionRequestPayload  `json:"question_request,omitempty"`
	QuestionAnswer   *QuestionAnswerPayload   `json:"question_answer,omitempty"`
	WorkTitle        *WorkTitleUpdatedPayload `json:"work_title,omitempty"`
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

// RunEventError 描述流执行中的终止或可恢复错误。
type RunEventError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// ---- payload 类型 ----

// UsagePayload 描述模型 token 使用情况。
type UsagePayload struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
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

// RuntimeTodoItem 描述一个运行时本地规划步骤。
type RuntimeTodoItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

// ArtifactPayload 引用单次运行产生的产物。
type ArtifactPayload struct {
	ArtifactID   string `json:"artifact_id,omitempty"`
	Title        string `json:"title,omitempty"`
	Filename     string `json:"filename,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	Description  string `json:"description,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	StorageKey   string `json:"storage_key,omitempty"`
	StorageURI   string `json:"storage_uri,omitempty"`
	Sha256       string `json:"sha256,omitempty"`
	Source       string `json:"source,omitempty"`
	Status       string `json:"status,omitempty"`
}

// ApprovalRequestPayload 描述需要用户审批的工具调用。
type ApprovalRequestPayload struct {
	RequestID   string            `json:"request_id"`
	ToolName    string            `json:"tool_name"`
	ToolCallID  string            `json:"tool_call_id,omitempty"`
	Description string            `json:"description"`
	Arguments   json.RawMessage   `json:"arguments,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ApprovalDecisionPayload 描述审批结果。
type ApprovalDecisionPayload struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"` // "approve" | "deny" | "always"
	Reason    string `json:"reason,omitempty"`
}

// QuestionRequestPayload 描述引擎向用户提出的澄清问题。
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

// QuestionItem 是问题请求中的单个问题。
type QuestionItem struct {
	Question    string           `json:"question"`
	Header      string           `json:"header,omitempty"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multiple"`
	Custom      bool             `json:"custom"`
}

// QuestionOption 是问题的选项。
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionAnswerPayload 描述用户对问题的回答。
type QuestionAnswerPayload struct {
	RequestID string     `json:"request_id"`
	Answers   [][]string `json:"answers"`
}

// ---- 终端事件 payload ----

// RunCompletedPayload 归档完整的成功运行。
type RunCompletedPayload struct {
	Status      string              `json:"status"`
	Result      RunResultPayload    `json:"result"`
	Artifacts   []ArtifactPayload   `json:"artifacts,omitempty"`
	Usage       *UsagePayload       `json:"usage,omitempty"`
	Events      []RunEventRecord    `json:"events,omitempty"`
	StartedAt   string              `json:"started_at,omitempty"`
	CompletedAt string              `json:"completed_at,omitempty"`
	Metadata    *RunMetadataPayload `json:"metadata,omitempty"`
}

// RunMetadataPayload contains typed run metadata while preserving the wire JSON object.
type RunMetadataPayload struct {
	Runtime    string `json:"runtime,omitempty"`
	WorkDir    string `json:"work_dir,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Resume     bool   `json:"resume,omitempty"`
}

// RunResultPayload 描述 run.completed 中归档的最终助手结果。
type RunResultPayload struct {
	Message string `json:"message,omitempty"`
}

// RunEventRecord 是归一化、已归档的运行时事件。
type RunEventRecord struct {
	Seq       int64           `json:"seq,omitempty"`
	LastSeq   int64           `json:"last_seq,omitempty"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}
