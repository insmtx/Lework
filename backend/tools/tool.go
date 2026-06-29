// tools 包定义 Leros 的最小 Tool 抽象。
//
// 当前阶段只提供基础接口和 agent 运行时注入的上下文信息。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Schema describes tool input or output in a provider-agnostic shape.
type Schema struct {
	Type       string               `json:"type"`
	Required   []string             `json:"required,omitempty"`
	Properties map[string]*Property `json:"properties,omitempty"`
}

// Property describes a single field inside a tool schema.
type Property struct {
	Type        string    `json:"type"`
	Description string    `json:"description,omitempty"`
	Enum        []string  `json:"enum,omitempty"`
	Items       *Property `json:"items,omitempty"`
}

// Tool 是 Leros 的最小工具接口。
type Tool interface {
	Name() string
	Description() string
	InputSchema() Schema
	Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// Validator is implemented by tools that perform local input validation before execution.
type Validator interface {
	Validate(input json.RawMessage) error
}

// BaseTool stores the LLM-facing metadata shared by concrete tools.
type BaseTool struct {
	name        string
	description string
	inputSchema Schema
}

// NewBaseTool creates a reusable metadata base for a concrete tool.
func NewBaseTool(name string, description string, inputSchema Schema) BaseTool {
	return BaseTool{
		name:        strings.TrimSpace(name),
		description: strings.TrimSpace(description),
		inputSchema: inputSchema,
	}
}

// Name returns the stable tool identifier.
func (t BaseTool) Name() string {
	return t.name
}

// Description returns the LLM-facing tool description.
func (t BaseTool) Description() string {
	return t.description
}

// InputSchema returns the tool argument schema.
func (t BaseTool) InputSchema() Schema {
	return t.inputSchema
}

// JSONString encodes structured tool output as the string payload returned to the model.
func JSONString(value interface{}) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal tool output: %w", err)
	}
	return string(encoded), nil
}

// JSONInput encodes a typed value at a tool protocol boundary.
func JSONInput(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

// DecodeInput decodes a tool JSON object for an implementation's internal use.
func DecodeInput(input json.RawMessage) (map[string]any, error) {
	if len(input) == 0 {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(input, &decoded); err != nil {
		return nil, fmt.Errorf("decode tool input: %w", err)
	}
	return decoded, nil
}

// ToolContext 携带 agent runtime 注入的 run 级身份与会话元数据。
type ToolContext struct {
	// RunID 是本次 agent 运行的唯一标识。
	RunID string
	// TraceID 是跨服务关联的分布式追踪标识。
	TraceID string
	// AssistantID 是执行本次运行的助手标识。
	AssistantID string
	// UserID 是发起本次运行的人类用户标识。
	UserID string
	// AccountID 是租户或组织账号标识。
	AccountID string
	// Channel 是接收请求的交互渠道（如 "github"、"slack"）。
	Channel string
	// ConversationID 是本次运行所属的会话标识。
	ConversationID string
	// ExternalID 是外部渠道中的发起者标识。
	ExternalID string
	// WorkNodeID 是执行本次运行时的工作节点标识。
	WorkNodeID string
	// WorkDir 是本次运行隔离工作区的绝对路径。
	WorkDir string
	// Metadata carries typed workspace data needed by business tools.
	Metadata ToolMetadata
}

// ToolMetadata contains optional run-scoped data shared with business tools.
type ToolMetadata struct {
	RepoDir              string
	ArtifactManifestPath string
}

type toolContextKey struct{}

// ContextWithToolContext stores run-scoped tool context on a context.Context.
func ContextWithToolContext(ctx context.Context, toolCtx ToolContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, toolContextKey{}, cloneToolContext(toolCtx))
}

// ToolContextFrom returns run-scoped tool context stored on ctx.
func ToolContextFrom(ctx context.Context) (ToolContext, bool) {
	if ctx == nil {
		return ToolContext{}, false
	}
	toolCtx, ok := ctx.Value(toolContextKey{}).(ToolContext)
	if !ok {
		return ToolContext{}, false
	}
	return cloneToolContext(toolCtx), true
}

// RequireToolContext returns run-scoped tool context or an error when it is missing.
func RequireToolContext(ctx context.Context) (ToolContext, error) {
	toolCtx, ok := ToolContextFrom(ctx)
	if !ok {
		return ToolContext{}, fmt.Errorf("tool context is required")
	}
	return toolCtx, nil
}

func cloneToolContext(toolCtx ToolContext) ToolContext {
	return toolCtx
}
