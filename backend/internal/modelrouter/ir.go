package modelrouter

// IRRole represents a message role in the IR.
type IRRole string

const (
	IRRoleUser      IRRole = "user"
	IRRoleAssistant IRRole = "assistant"
	IRRoleSystem    IRRole = "system"
	IRRoleTool      IRRole = "tool"
)

// IRPartType represents the type of a content part.
type IRPartType string

const (
	IRPartText       IRPartType = "text"
	IRPartToolCall   IRPartType = "tool_call"
	IRPartToolResult IRPartType = "tool_result"
	IRPartReasoning  IRPartType = "reasoning"
	IRPartRefusal    IRPartType = "refusal"
	IRPartImage      IRPartType = "image"
	IRPartAudio      IRPartType = "audio"
	IRPartFile       IRPartType = "file"
)

// IRStreamEventType represents the type of a streaming event.
type IRStreamEventType string

const (
	IRStreamMessageStart  IRStreamEventType = "message_start"
	IRStreamContentStart  IRStreamEventType = "content_part_start"
	IRStreamContentDelta  IRStreamEventType = "content_part_delta"
	IRStreamContentStop   IRStreamEventType = "content_part_stop"
	IRStreamMessageDelta  IRStreamEventType = "message_delta"
	IRStreamDone          IRStreamEventType = "done"
	IRStreamError         IRStreamEventType = "error"
)

// IRStopReason represents why a response stopped.
type IRStopReason string

const (
	IRStopEndTurn       IRStopReason = "end_turn"
	IRStopToolUse       IRStopReason = "tool_use"
	IRStopStopSequence  IRStopReason = "stop_sequence"
	IRStopMaxTokens     IRStopReason = "max_tokens"
	IRStopContentFilter IRStopReason = "content_filter"
	IRStopLength        IRStopReason = "length"
	IRStopError         IRStopReason = "error"
)

// IRContentPart is a single part within a message.
type IRContentPart struct {
	ID         string            `json:"id,omitempty"`
	Type       IRPartType        `json:"type"`
	Text       string            `json:"text,omitempty"`
	ToolCall   *IRToolCallPart   `json:"tool_call,omitempty"`
	ToolResult *IRToolResultPart `json:"tool_result,omitempty"`
	Reasoning  *IRReasoningPart  `json:"reasoning,omitempty"`
	Refusal    *IRRefusalPart    `json:"refusal,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// IRToolCallPart holds tool invocation data.
type IRToolCallPart struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	ArgumentsRaw  string                 `json:"arguments_raw,omitempty"`
	ArgumentsJSON map[string]interface{} `json:"arguments_json,omitempty"`
	Status        string                 `json:"status,omitempty"` // in_progress, completed, failed
}

// IRToolResultPart holds the result of a tool execution.
type IRToolResultPart struct {
	ToolCallID string          `json:"tool_call_id"`
	Content    []IRContentPart `json:"content,omitempty"`
	Status     string          `json:"status,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// IRReasoningPart holds model reasoning/thinking content.
type IRReasoningPart struct {
	Content   string `json:"content"`
	Signature string `json:"signature,omitempty"`
}

// IRRefusalPart holds model refusal content.
type IRRefusalPart struct {
	Text string `json:"text"`
}

// IRMessage is a single message in a conversation.
type IRMessage struct {
	ID    string          `json:"id,omitempty"`
	Role  IRRole          `json:"role"`
	Name  string          `json:"name,omitempty"`
	Parts []IRContentPart `json:"parts,omitempty"`
}

// GetTextContent returns concatenated text from all text parts.
func (m IRMessage) GetTextContent() string {
	var s string
	for _, p := range m.Parts {
		if p.Type == IRPartText {
			s += p.Text
		}
	}
	return s
}

// IRToolDecl is a tool definition in the IR.
type IRToolDecl struct {
	Type        string      `json:"type"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"` // JSON Schema — acceptable here
}

// IRToolChoice represents how tool selection is handled.
type IRToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// IRResponseFormat represents the response format configuration.
type IRResponseFormat struct {
	Type       string                 `json:"type"`
	JSONSchema map[string]interface{} `json:"json_schema,omitempty"`
}

// IRUsage represents token usage.
type IRUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`

	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`

	ReasoningTokens int `json:"reasoning_tokens,omitempty"`

	ProviderRaw map[string]interface{} `json:"-"`
}

// IRRequest is the canonical request in the IR.
type IRRequest struct {
	ID              string                            `json:"id,omitempty"`
	Model           string                            `json:"model"`
	Stream          bool                              `json:"stream,omitempty"`
	User            string                            `json:"user,omitempty"`
	System          string                            `json:"system,omitempty"`
	Instructions    string                            `json:"instructions,omitempty"`
	Messages        []IRMessage                       `json:"messages,omitempty"`
	Tools           []IRToolDecl                      `json:"tools,omitempty"`
	ToolChoice      *IRToolChoice                     `json:"tool_choice,omitempty"`
	ResponseFormat  *IRResponseFormat                 `json:"response_format,omitempty"`
	ReasoningEffort string                            `json:"reasoning_effort,omitempty"`
	MaxTokens       int                               `json:"max_tokens,omitempty"`
	Temperature     *float64                          `json:"temperature,omitempty"`
	TopP            *float64                          `json:"top_p,omitempty"`
	Stop            []string                          `json:"stop,omitempty"`
	Seed            *int                              `json:"seed,omitempty"`
	Extensions      map[string]map[string]interface{} `json:"-"` // per-protocol, not serialized
}

// IRResponse is the canonical response in the IR.
type IRResponse struct {
	ID         string                            `json:"id"`
	Model      string                            `json:"model"`
	Created    int64                             `json:"created,omitempty"`
	Content    []IRContentPart                   `json:"content,omitempty"`
	Usage      *IRUsage                          `json:"usage,omitempty"`
	StopReason IRStopReason                      `json:"stop_reason,omitempty"`
	Extensions map[string]map[string]interface{} `json:"-"`
}

// IRStreamEvent is a single streaming event in the IR.
type IRStreamEvent struct {
	Type          IRStreamEventType `json:"type"`
	ResponseID    string            `json:"response_id,omitempty"`
	ResponseModel string            `json:"response_model,omitempty"`
	Index         int               `json:"index,omitempty"`
	Part          *IRContentPart    `json:"part,omitempty"`
	DeltaText     string            `json:"delta_text,omitempty"`
	DeltaJSON     string            `json:"delta_json,omitempty"`
	DeltaType     string            `json:"delta_type,omitempty"`
	StopReason    IRStopReason      `json:"stop_reason,omitempty"`
	Usage         *IRUsage          `json:"usage,omitempty"`
	ErrorMessage  string            `json:"error_message,omitempty"`
	ErrorType     string            `json:"error_type,omitempty"`
}
