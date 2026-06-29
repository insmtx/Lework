// Package opencode 将 OpenCode CLI 适配到 Leros 外部 CLI 引擎接口。
// 使用 opencode serve 模式，通过 HTTP REST API + SSE 进行通信。
package opencode

// ============================================================================
// HTTP API 类型定义
// ============================================================================

// sessionCreateRequest 是 POST /session 的请求体。
type sessionCreateRequest struct {
	ParentID   string           `json:"parentID,omitempty"`
	Title      string           `json:"title,omitempty"`
	Agent      string           `json:"agent,omitempty"`
	Model      *sessionModelRef `json:"model,omitempty"`
	Permission any              `json:"permission,omitempty"`
}

// sessionModelRef 是 session 创建和消息发送时的模型引用格式。
// 注意：session 创建和 message 发送时 model 字段的格式不同：
// - session 创建: {providerID, id}
// - message 发送: {providerID, modelID}
type sessionModelRef struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	ID         string `json:"id,omitempty"`
}

// sessionResponse 是 POST /session 的响应体。
type sessionResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"createdAt"`
}

// messageRequest 是 POST /session/:id/message 的请求体。
type messageRequest struct {
	MessageID string           `json:"messageID,omitempty"`
	Model     *sessionModelRef `json:"model,omitempty"`
	Agent     string           `json:"agent,omitempty"`
	System    string           `json:"system,omitempty"`
	NoReply   bool             `json:"noReply,omitempty"`
	Parts     []messagePart    `json:"parts"`
}

// messagePart 是消息中的单个内容部分。
type messagePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// messageResponse 是 POST /session/:id/message 的响应体。
type messageResponse struct {
	Info  messageInfo       `json:"info"`
	Parts []messagePartResp `json:"parts"`
}

// messageInfo 是消息的元信息。
type messageInfo struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
}

// messagePartResp 是消息响应中的一个部分。
type messagePartResp struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	ToolCallID string `json:"toolCallID,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	Args       any    `json:"args,omitempty"`
	Result     any    `json:"result,omitempty"`
	IsError    bool   `json:"isError,omitempty"`
	Output     string `json:"output,omitempty"`
}

// ============================================================================
// SSE 事件类型
// ============================================================================

// sseEvent 是从 SSE 流解析的原始事件。
type sseEvent struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Properties any    `json:"properties,omitempty"`
}

// textDeltaProps 是 session.next.text.delta 事件的 properties。
type textDeltaProps struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	TextID             string `json:"textID"`
	Delta              string `json:"delta"`
}

// textStartedProps 是 session.next.text.started 事件的 properties。
type textStartedProps struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	TextID             string `json:"textID"`
}

// textEndedProps 是 session.next.text.ended 事件的 properties。
type textEndedProps struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	TextID             string `json:"textID"`
	Text               string `json:"text"`
}

// toolCalledProps 是 session.next.tool.called 事件的 properties。
type toolCalledProps struct {
	SessionID          string         `json:"sessionID"`
	AssistantMessageID string         `json:"assistantMessageID"`
	CallID             string         `json:"callID"`
	Tool               string         `json:"tool"`
	Input              map[string]any `json:"input"`
}

// toolSuccessProps 是 session.next.tool.success 事件的 properties。
type toolSuccessProps struct {
	SessionID          string   `json:"sessionID"`
	AssistantMessageID string   `json:"assistantMessageID"`
	CallID             string   `json:"callID"`
	Tool               string   `json:"tool"`
	Result             any      `json:"result,omitempty"`
	OutputPaths        []string `json:"outputPaths,omitempty"`
}

// toolFailedProps 是 session.next.tool.failed 事件的 properties。
type toolFailedProps struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	CallID             string `json:"callID"`
	Tool               string `json:"tool"`
	Error              struct {
		Message string `json:"message"`
	} `json:"error"`
}

// stepEndedProps 是 session.next.step.ended 事件的 properties。
type stepEndedProps struct {
	SessionID          string  `json:"sessionID"`
	AssistantMessageID string  `json:"assistantMessageID"`
	Finish             string  `json:"finish"`
	Cost               float64 `json:"cost"`
	Tokens             struct {
		Input  int `json:"input"`
		Output int `json:"output"`
		Cache  struct {
			Read  int `json:"read"`
			Write int `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

// reasoningDeltaProps 是 session.next.reasoning.delta 事件的 properties。
type reasoningDeltaProps struct {
	SessionID          string `json:"sessionID"`
	AssistantMessageID string `json:"assistantMessageID"`
	ReasoningID        string `json:"reasoningID"`
	Delta              string `json:"delta"`
}

// ============================================================================
// OPENCODE_CONFIG_CONTENT 类型
// ============================================================================

// configContent 是 OPENCODE_CONFIG_CONTENT 的顶层结构。
type configContent struct {
	Schema     string                    `json:"$schema,omitempty"`
	Provider   map[string]providerConfig `json:"provider"`
	Model      string                    `json:"model,omitempty"`
	Permission map[string]string         `json:"permission,omitempty"`
	MCP        map[string]any            `json:"mcp,omitempty"`
}

// providerConfig 描述一个 AI provider 的配置。
type providerConfig struct {
	ID      string                 `json:"id"`
	Npm     string                 `json:"npm"`
	Options providerOptions        `json:"options"`
	Models  map[string]modelConfig `json:"models"`
}

// providerOptions 包含 provider 的连接配置。
type providerOptions struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseURL"`
	Timeout int    `json:"timeout,omitempty"`
}

// modelConfig 描述单个模型的配置。
type modelConfig struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Limit       modelLimit `json:"limit"`
	Cost        modelCost  `json:"cost"`
	ToolCall    bool       `json:"tool_call"`
	Attachment  bool       `json:"attachment"`
	Reasoning   bool       `json:"reasoning"`
	Temperature bool       `json:"temperature"`
}

// modelLimit 描述模型的上下文限制。
type modelLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// modelCost 描述模型的费用。
type modelCost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// ============================================================================
// 全局 Health API
// ============================================================================

// healthResponse 是 GET /global/health 的响应体。
type healthResponse struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
}

// ============================================================================
// 权限 SSE 事件
// ============================================================================

// permissionAskedProps 是 permission.asked 事件的 properties。
// 对应 OpenCode PermissionV1.Request 结构。
type permissionAskedProps struct {
	SessionID  string          `json:"sessionID"`
	ID         string          `json:"id"`
	Permission string          `json:"permission"`
	Patterns   []string        `json:"patterns"`
	Metadata   map[string]any  `json:"metadata"`
	Always     []string        `json:"always"`
	Tool       *permissionTool `json:"tool,omitempty"`
}

// permissionTool 是权限请求中关联的工具调用信息。
type permissionTool struct {
	MessageID string `json:"messageID"`
	CallID    string `json:"callID"`
}

// ============================================================================
// 权限响应
// ============================================================================

// permissionDecision 是 POST /permission/:requestID/reply 的请求体。
type permissionDecision struct {
	Reply   string `json:"reply"`
	Message string `json:"message,omitempty"`
}

// ============================================================================
// Question SSE 事件类型（question.v2.asked / question.asked）
// ============================================================================

// questionAskedProps 是 question.v2.asked 事件的 properties。
// 对应 OpenCode QuestionV2.Ask 结构。
type questionAskedProps struct {
	SessionID string            `json:"sessionID"`
	ID        string            `json:"id"`
	Questions []questionItem    `json:"questions"`
	Tool      *questionToolInfo `json:"tool,omitempty"`
}

// questionItem 是单个问题的结构。
type questionItem struct {
	Question string           `json:"question"`
	Header   string           `json:"header,omitempty"`
	Options  []questionOption `json:"options"`
	Multiple bool             `json:"multiple"`
	Custom   bool             `json:"custom"`
}

// questionOption 是问题的单个选项。
type questionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// questionToolInfo 是 question 事件中关联的工具调用信息。
type questionToolInfo struct {
	MessageID string `json:"messageID"`
	CallID    string `json:"callID"`
}

// questionAnswerReq 是 POST /question/:requestID/reply 的请求体。
type questionAnswerReq struct {
	Answers [][]string `json:"answers"`
}

// ============================================================================
// Todo SSE 事件类型
// ============================================================================

// todoUpdatedProps 是 todo.updated 事件的 properties。
type todoUpdatedProps struct {
	SessionID string             `json:"sessionID"`
	Todos     []opencodeTodoItem `json:"todos"`
}

// opencodeTodoItem 是 OpenCode 格式的单个待办项。
type opencodeTodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}
