package modelrouter

// IRBlockType is the canonical block type in the Intermediate Representation.
type IRBlockType string

const (
	IRBlockText             IRBlockType = "text"
	IRBlockToolUse          IRBlockType = "tool_use"
	IRBlockToolResult       IRBlockType = "tool_result"
	IRBlockThinking         IRBlockType = "thinking"
	IRBlockRedactedThinking IRBlockType = "redacted_thinking"
)

// IRThinkingBlock holds thinking content and optional signature.
type IRThinkingBlock struct {
	Content   string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

// IRContentBlock is a single content block in the canonical IR.
type IRContentBlock struct {
	Type    IRBlockType      `json:"type"`
	Text    string           `json:"text,omitempty"`
	Thinking *IRThinkingBlock `json:"thinking_block,omitempty"`

	// ToolUse fields
	ToolUseID    string      `json:"tool_use_id,omitempty"`
	ToolUseName  string      `json:"tool_use_name,omitempty"`
	ToolUseInput interface{} `json:"tool_use_input,omitempty"`

	// ToolResult fields
	ToolResultToolUseID string      `json:"tool_result_tool_use_id,omitempty"`
	ToolResultContent   interface{} `json:"tool_result_content,omitempty"`
	IsError             bool        `json:"is_error,omitempty"`
}

// IRError represents a canonical error in the IR.
type IRError struct {
	Type    IRErrorType `json:"type"`
	Code    string      `json:"code,omitempty"`
	Message string      `json:"message"`
	// HTTP status code for REST transport
	StatusCode int `json:"status_code,omitempty"`
	// Original upstream error body, preserved verbatim for passthrough scenarios
	UpstreamBody []byte `json:"-"`
}

// IRErrorType categorizes errors in the canonical IR.
type IRErrorType string

const (
	IRErrorInvalidRequest     IRErrorType = "invalid_request"
	IRErrorAuthentication     IRErrorType = "authentication"
	IRErrorPermission         IRErrorType = "permission"
	IRErrorNotFound           IRErrorType = "not_found"
	IRErrorRateLimit          IRErrorType = "rate_limit"
	IRErrorQuotaExceeded      IRErrorType = "quota_exceeded"
	IRErrorServerError        IRErrorType = "server_error"
	IRErrorServiceUnavailable IRErrorType = "service_unavailable"
	IRErrorTimeout            IRErrorType = "timeout"
	IRErrorUpstreamError      IRErrorType = "upstream_error"
	IRErrorModelError         IRErrorType = "model_error"
	IRErrorContentFilter      IRErrorType = "content_filter"
	IRErrorToolExecution      IRErrorType = "tool_execution"
	IRErrorContextLength      IRErrorType = "context_length"
	IRErrorUnknown            IRErrorType = "unknown"
)

// IRRole represents a message role in the canonical IR.
type IRRole string

const (
	IRRoleUser      IRRole = "user"
	IRRoleAssistant IRRole = "assistant"
	IRRoleSystem    IRRole = "system"
	IRRoleTool      IRRole = "tool"
)

// IRToolType is the type of a tool definition.
type IRToolType string

const (
	IRToolFunction IRToolType = "function"
)

// IRToolDecl is a tool definition in the canonical IR.
type IRToolDecl struct {
	Type        IRToolType  `json:"type"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// IRToolChoice represents a tool choice.
type IRToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// IRStreamEventType is the type of a streaming event.
type IRStreamEventType string

const (
	IRStreamMessageStart IRStreamEventType = "message_start"
	IRStreamContentStart IRStreamEventType = "content_block_start"
	IRStreamContentDelta IRStreamEventType = "content_block_delta"
	IRStreamContentStop  IRStreamEventType = "content_block_stop"
	IRStreamMessageDelta IRStreamEventType = "message_delta"
	IRStreamDone         IRStreamEventType = "done"
	IRStreamError        IRStreamEventType = "error"
)

// IRUsage represents token usage in canonical form.
type IRUsage struct {
	InputTokens       int `json:"input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
	ReasoningTokens   int `json:"reasoning_tokens,omitempty"`
	TotalTokens       int `json:"total_tokens,omitempty"`
}

// IRStopReason maps model-specific stop reasons to canonical ones.
type IRStopReason string

const (
	IRStopEndTurn        IRStopReason = "end_turn"
	IRStopToolUse        IRStopReason = "tool_use"
	IRStopStopSequence   IRStopReason = "stop_sequence"
	IRStopMaxTokens      IRStopReason = "max_tokens"
	IRStopContentFilter  IRStopReason = "content_filter"
	IRStopLength         IRStopReason = "length"
	IRStopError          IRStopReason = "error"
)

// IRStreamEvent is a single streaming event in the canonical IR.
type IRStreamEvent struct {
	Type          IRStreamEventType
	Index         int
	ResponseID    string
	ResponseModel string
	ContentBlock  *IRContentBlock

	DeltaType string
	DeltaText string
	DeltaJSON string

	StopReason IRStopReason
	Usage      *IRUsage

	// Error-specific fields (preserved from upstream)
	ErrorMessage string
	ErrorType    string // upstream error type (e.g. "invalid_request_error", "api_error")
	ErrorCode    string // upstream error code (e.g. "rate_limit_exceeded", "context_length_exceeded")
}

// IRMessage is a message in the canonical IR.
type IRMessage struct {
	Role     IRRole                    `json:"role"`
	Content  []IRContentBlock          `json:"content,omitempty"`
	Name     string                    `json:"name,omitempty"`
	Preserved map[string]interface{}   `json:"-"`
}

// getTextContent returns the concatenated text content of all text blocks.
func (m IRMessage) getTextContent() string {
	var s string
	for _, b := range m.Content {
		if b.Type == IRBlockText {
			s += b.Text
		}
	}
	return s
}

// IRRequest is the canonical request in the Intermediate Representation.
type IRRequest struct {
	Model           string                 `json:"model"`
	Messages        []IRMessage            `json:"messages,omitempty"`
	System          string                 `json:"system,omitempty"`
	Instructions    string                 `json:"instructions,omitempty"`
	MaxTokens       int                    `json:"max_tokens,omitempty"`
	Temperature     *float64               `json:"temperature,omitempty"`
	TopP            *float64               `json:"top_p,omitempty"`
	Stop            []string               `json:"stop,omitempty"`
	Tools           []IRToolDecl           `json:"tools,omitempty"`
	ToolChoice      *IRToolChoice          `json:"tool_choice,omitempty"`
	Stream          bool                   `json:"stream,omitempty"`
	Seed            *int                   `json:"seed,omitempty"`
	User            string                 `json:"user,omitempty"`
	Metadata        interface{}            `json:"metadata,omitempty"`
	Preserved       map[string]interface{} `json:"-"`

	// Unified provider fields
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
	ResponseFormat  *IRResponseFormat `json:"response_format,omitempty"`
}

// IRResponseFormat represents the response_format parameter.
type IRResponseFormat struct {
	Type       string                 `json:"type"`
	JSONSchema map[string]interface{} `json:"json_schema,omitempty"`
}

// IRResponse is the canonical response in the Intermediate Representation.
type IRResponse struct {
	ID           string           `json:"id"`
	Model        string           `json:"model"`
	Created      int64            `json:"created,omitempty"`
	StopReason   IRStopReason     `json:"stop_reason,omitempty"`
	StopSequence string           `json:"stop_sequence,omitempty"`
	Content      []IRContentBlock `json:"content,omitempty"`
	Usage        *IRUsage         `json:"usage,omitempty"`
	Preserved    map[string]interface{} `json:"-"`

	// Unified provider fields
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	ResponseFormat  *IRResponseFormat `json:"response_format,omitempty"`
}
