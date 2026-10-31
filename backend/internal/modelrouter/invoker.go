package modelrouter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bytedance/sonic"

	"github.com/insmtx/Lework/backend/internal/llm"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Invoker — inter-process structured LLM call interface
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Invoker 定义进程内 LLM 调用入口，供 service 层依赖。
// 实现方通过 llm.Manager 从 DB 解析模型配置，通过 llm.Caller 发起 HTTP 调用。
type Invoker interface {
	Call(ctx context.Context, orgID uint, req *llm.CallRequest, opts ...InvokeOption) (*llm.CallResult, error)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ModelRouter — default Invoker implementation
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ModelRouter 是 Invoker 的默认实现，持有进程级不变的 Manager 和 Caller。
type ModelRouter struct {
	manager llm.Manager
	caller  llm.Caller
}

// NewModelRouter creates a ModelRouter with the given manager and caller.
func NewModelRouter(manager llm.Manager, caller llm.Caller) *ModelRouter {
	return &ModelRouter{manager: manager, caller: caller}
}

var _ Invoker = (*ModelRouter)(nil)

// InvokeOption 配置 Call() 的行为选项。
type InvokeOption func(*invokeOptions)

type invokeOptions struct {
	modelCode string
	modelID   uint
}

// WithModelCode 按 llm_models.code 指定要使用的模型。
// 不调用此选项时 Call() 使用组织默认模型。
func WithModelCode(code string) InvokeOption {
	return func(o *invokeOptions) { o.modelCode = code }
}

// WithModelID 按 llm_models.id 指定要使用的模型。
func WithModelID(id uint) InvokeOption {
	return func(o *invokeOptions) { o.modelID = id }
}

// Call 执行一次进程内结构化 LLM 调用，使用选项模式选择模型。
// 优先级：WithModelID > WithModelCode > 组织默认模型。
func (r *ModelRouter) Call(ctx context.Context, orgID uint, req *llm.CallRequest, opts ...InvokeOption) (*llm.CallResult, error) {
	var o invokeOptions
	for _, opt := range opts {
		opt(&o)
	}

	if r.manager == nil {
		return nil, errors.New("modelrouter: manager not configured")
	}

	var cfg *llm.ModelConfig
	var err error
	switch {
	case o.modelID != 0:
		cfg, err = r.manager.GetByModelID(ctx, orgID, o.modelID)
	case o.modelCode != "":
		cfg, err = r.manager.GetByModelCode(ctx, orgID, o.modelCode)
	default:
		cfg, err = r.manager.GetDefault(ctx, orgID)
	}
	if err != nil {
		return nil, fmt.Errorf("modelrouter: resolve model: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("modelrouter: no model config found")
	}

	if r.caller == nil {
		return nil, errors.New("modelrouter: caller not configured")
	}

	if req.CallerType != "" {
		ctx = llm.WithCtxString(ctx, llm.CtxCallerType, req.CallerType)
	}
	ctx = llm.WithCtxString(ctx, llm.CtxReqID, req.ReqID)
	ctx = llm.WithCtxUint(ctx, llm.CtxProjectID, req.ProjectID)
	ctx = llm.WithCtxUint(ctx, llm.CtxSessionID, req.SessionID)
	ctx = llm.WithCtxUint(ctx, llm.CtxMessageID, req.MessageID)
	ctx = llm.WithCtxUint(ctx, llm.CtxAssistantID, req.AssistantID)
	ctx = llm.WithCtxUint(ctx, llm.CtxUin, req.Uin)
	if ip := llm.GetCtxString(ctx, llm.CtxClientIP); ip != "" {
		ctx = llm.WithCtxString(ctx, llm.CtxClientIP, ip)
	}

	body, err := buildChatCompletionBody(cfg.ModelName, req)
	if err != nil {
		return nil, fmt.Errorf("modelrouter: build request body: %w", err)
	}

	result, err := r.caller.CallRaw(ctx, orgID, cfg, body)
	if err != nil {
		return nil, err
	}

	parseChatCompletionResponse(result)
	return result, nil
}

// buildChatCompletionBody 将 llm.CallRequest 序列化为 OpenAI Chat Completion JSON body。
// 通过 sonic 桥接来避免 modelrouter 直接依赖 eino 类型。
func buildChatCompletionBody(modelName string, req *llm.CallRequest) ([]byte, error) {
	messages := make([]map[string]interface{}, 0, len(req.Messages)+1)

	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": req.SystemPrompt,
		})
	}

	for _, msg := range req.Messages {
		messages = append(messages, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	body := map[string]interface{}{
		"model":    modelName,
		"messages": messages,
		"stream":   false,
	}

	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		body["max_tokens"] = *req.MaxTokens
	}

	// 桥接 eino 类型：通过 marshal/unmarshal 提取 ResponseFormat/ReasoningEffort
	if req.ResponseFormat != nil || req.ReasoningEffort != "" {
		rawJSON, err := sonic.Marshal(req)
		if err == nil {
			var raw map[string]interface{}
			if err := sonic.Unmarshal(rawJSON, &raw); err == nil {
				if v, ok := raw["response_format"]; ok && v != nil {
					body["response_format"] = v
				}
				if v, ok := raw["reasoning_effort"]; ok && v != nil {
					body["reasoning_effort"] = v
				}
			}
		}
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(req.Tools))
		for _, t := range req.Tools {
			tool := map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
				},
			}
			if t.JSONSchema != nil {
				tool["function"].(map[string]interface{})["parameters"] = t.JSONSchema
			}
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}

	return sonic.Marshal(body)
}

// parseChatCompletionResponse 从 OpenAI Chat Completion 响应 JSON 中
// 提取 content 和 usage，填充到 result.Message 和 result.Usage。
func parseChatCompletionResponse(result *llm.CallResult) {
	if result == nil || len(result.RawResponseBody) == 0 {
		return
	}

	var raw map[string]interface{}
	if err := sonic.Unmarshal(result.RawResponseBody, &raw); err != nil {
		return
	}

	if choices, ok := raw["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					result.Message = &llm.SchemaMessage{Content: content}
				}
			}
		}
	}

	if usageRaw, ok := raw["usage"].(map[string]interface{}); ok {
		usage := &llm.Usage{}
		if v, ok := usageRaw["prompt_tokens"].(float64); ok {
			usage.InputTokens = int(v)
		}
		if v, ok := usageRaw["completion_tokens"].(float64); ok {
			usage.OutputTokens = int(v)
		}
		if v, ok := usageRaw["total_tokens"].(float64); ok {
			usage.TotalTokens = int(v)
		}
		result.Usage = usage
	}
}
