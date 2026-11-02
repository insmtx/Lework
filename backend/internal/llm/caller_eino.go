package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	einoschema "github.com/cloudwego/eino/schema"
	"github.com/ygpkg/yg-go/logs"

	pkgeino "github.com/insmtx/Lework/backend/pkg/eino"
)

// CallerEino 是基于 eino 框架的 Caller 接口实现。
type CallerEino struct {
	manager  Manager
	recorder Recorder
}

// NewCaller 创建一个基于 eino 的 Caller 实现。
func NewCaller(manager Manager, recorder Recorder) *CallerEino {
	return &CallerEino{manager: manager, recorder: recorder}
}

var _ Caller = (*CallerEino)(nil)

// Call 执行一次非流式 LLM 调用。
func (c *CallerEino) Call(ctx context.Context, orgID uint, req *CallRequest) (*CallResult, error) {
	startedAt := time.Now()

	cfg, err := c.manager.Get(ctx, orgID, req.ModelID, "")
	if err != nil {
		return nil, fmt.Errorf("resolve model config: %w", err)
	}

	chatModel, err := buildEinoChatModel(ctx, cfg, req)
	if err != nil {
		return nil, err
	}

	messages := buildEinoMessages(req)

	resp, err := chatModel.Generate(ctx, messages)
	finishedAt := time.Now()
	latencyMS := finishedAt.Sub(startedAt).Milliseconds()

	usage := extractUsage(resp)

	inputData, _ := json.Marshal(messages)
	outputData, _ := json.Marshal(resp)

	record := &CallRecord{
		OrgID:         orgID,
		ModelID:       cfg.ID,
		Provider:      cfg.Provider,
		ModelName:     cfg.ModelName,
		EntryProtocol: "eino_chat",
		IsStream:      false,
		LatencyMS:     latencyMS,
		Success:       err == nil,
		CallerType:    req.CallerType,
		ReqID:         req.ReqID,
		ProjectID:     req.ProjectID,
		SessionID:     req.SessionID,
		MessageID:     req.MessageID,
		AssistantID:   req.AssistantID,
		Uin:           req.Uin,
		ClientIP:      GetCtxString(ctx, CtxClientIP),
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		Input:         string(inputData),
		Output:        string(outputData),
		InputLen:      len(inputData),
		OutputLen:     len(outputData),
	}
	if usage != nil {
		record.InputTokens = usage.InputTokens
		record.OutputTokens = usage.OutputTokens
		record.TotalTokens = usage.TotalTokens
		record.PromptTokens = usage.PromptTokens
		record.CacheHitTokens = usage.CacheHitTokens
		record.CacheMissTokens = usage.CacheMissTokens
	}
	if err != nil {
		record.Message = err.Error()
		c.recordCall(ctx, record)
		return nil, err
	}

	c.recordCall(ctx, record)

	return &CallResult{
		Message: resp,
		Usage:   usage,
		Record:  record,
	}, nil
}

// Stream 执行一次流式 LLM 调用。
func (c *CallerEino) Stream(ctx context.Context, orgID uint, req *CallRequest, sink StreamSink) (*CallResult, error) {
	startedAt := time.Now()

	cfg, err := c.manager.Get(ctx, orgID, req.ModelID, "")
	if err != nil {
		return nil, fmt.Errorf("resolve model config: %w", err)
	}

	chatModel, err := buildEinoChatModel(ctx, cfg, req)
	if err != nil {
		return nil, err
	}

	flow, err := pkgeino.NewFlow(ctx, &pkgeino.FlowConfig{
		Model:        chatModel,
		SystemPrompt: req.SystemPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("create eino flow: %w", err)
	}

	userInput := ""
	if len(req.Messages) > 0 {
		userInput = req.Messages[len(req.Messages)-1].Content
	}

	contentBuf := &strings.Builder{}
	adapter := &streamSinkAdapter{sink: sink, contentBuf: contentBuf}
	resp, flowUsage, err := flow.StreamWithUsage(ctx, userInput, adapter)
	finishedAt := time.Now()
	latencyMS := finishedAt.Sub(startedAt).Milliseconds()

	usage := flowUsageToDomain(flowUsage)

	inputData, _ := json.Marshal(req.Messages)

	record := &CallRecord{
		OrgID:         orgID,
		ModelID:       cfg.ID,
		Provider:      cfg.Provider,
		ModelName:     cfg.ModelName,
		EntryProtocol: "eino_flow_stream",
		IsStream:      true,
		LatencyMS:     latencyMS,
		Success:       err == nil,
		CallerType:    req.CallerType,
		ReqID:         req.ReqID,
		ProjectID:     req.ProjectID,
		SessionID:     req.SessionID,
		MessageID:     req.MessageID,
		AssistantID:   req.AssistantID,
		Uin:           req.Uin,
		ClientIP:      GetCtxString(ctx, CtxClientIP),
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		Input:         string(inputData),
		Output:        contentBuf.String(),
		InputLen:      len(inputData),
		OutputLen:     contentBuf.Len(),
	}
	if usage != nil {
		record.InputTokens = usage.InputTokens
		record.OutputTokens = usage.OutputTokens
		record.TotalTokens = usage.TotalTokens
		record.PromptTokens = usage.PromptTokens
		record.CacheHitTokens = usage.CacheHitTokens
		record.CacheMissTokens = usage.CacheMissTokens
	}
	if err != nil {
		record.Message = err.Error()
		c.recordCall(ctx, record)
		return nil, err
	}

	c.recordCall(ctx, record)

	return &CallResult{
		Message: resp,
		Usage:   usage,
		Record:  record,
	}, nil
}

// recordCall 持久化调用记录，失败时仅记录警告不阻塞主流程。
func (c *CallerEino) recordCall(ctx context.Context, record *CallRecord) {
	if c.recorder == nil {
		return
	}
	if err := c.recorder.RecordCall(ctx, record); err != nil {
		logs.WarnContextf(ctx, "[WARN] failed to record llm call: %v", err)
	}
}

// buildEinoChatModel 根据模型配置和请求参数构建 eino ToolCallingChatModel。
func buildEinoChatModel(ctx context.Context, cfg *ModelConfig, req *CallRequest) (einomodel.ToolCallingChatModel, error) {
	endpoint := BuildLLMEndpointURL(cfg.BaseURL, cfg.BaseURLHasV1)

	einoCfg := &pkgeino.ChatModelConfig{
		Provider: cfg.Provider,
		APIKey:   cfg.APIKey,
		Model:    cfg.ModelName,
		BaseURL:  endpoint,
	}

	if req.Temperature != nil {
		temp := float32(*req.Temperature)
		einoCfg.Temperature = &temp
	} else if cfg.Temperature > 0 {
		temp := float32(cfg.Temperature)
		einoCfg.Temperature = &temp
	}

	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		einoCfg.MaxTokens = *req.MaxTokens
	} else if cfg.MaxTokens > 0 {
		einoCfg.MaxTokens = cfg.MaxTokens
	}

	einoCfg.ResponseFormat = req.ResponseFormat
	einoCfg.ReasoningEffort = req.ReasoningEffort

	chatModel, err := pkgeino.NewChatModel(ctx, einoCfg)
	if err != nil {
		return nil, fmt.Errorf("create eino chat model: %w", err)
	}
	return chatModel, nil
}

// buildEinoMessages 将 llm.Message 列表转换为 eino schema.Message 列表。
// 当 req.SystemPrompt 非空时在最前面插入一条 system 消息。
func buildEinoMessages(req *CallRequest) []*einoschema.Message {
	var messages []*einoschema.Message

	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, &einoschema.Message{
			Role:    einoschema.System,
			Content: req.SystemPrompt,
		})
	}

	for _, msg := range req.Messages {
		messages = append(messages, &einoschema.Message{
			Role:    mapRole(msg.Role),
			Content: msg.Content,
		})
	}

	return messages
}

// mapRole 将字符串角色映射为 eino schema.RoleType。
func mapRole(role string) einoschema.RoleType {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system":
		return einoschema.System
	case "assistant":
		return einoschema.Assistant
	case "tool":
		return einoschema.Tool
	default:
		return einoschema.User
	}
}

// extractUsage 从 eino schema.Message 的 ResponseMeta 中提取 token 用量。
func extractUsage(resp *einoschema.Message) *Usage {
	if resp == nil || resp.ResponseMeta == nil || resp.ResponseMeta.Usage == nil {
		return nil
	}
	u := resp.ResponseMeta.Usage
	total := u.TotalTokens
	if total == 0 {
		total = u.PromptTokens + u.CompletionTokens
	}
	return &Usage{
		InputTokens:    u.PromptTokens,
		OutputTokens:   u.CompletionTokens,
		TotalTokens:    total,
		PromptTokens:   int64(u.PromptTokens),
		CacheHitTokens: int64(u.PromptTokenDetails.CachedTokens),
	}
}

// flowUsageToDomain 将 pkgeino.Usage 转换为领域类型 Usage。
func flowUsageToDomain(u *pkgeino.Usage) *Usage {
	if u == nil {
		return nil
	}
	return &Usage{
		InputTokens:     u.InputTokens,
		OutputTokens:    u.OutputTokens,
		TotalTokens:     u.TotalTokens,
		PromptTokens:    u.PromptTokens,
		CacheHitTokens:  u.CacheHitTokens,
		CacheMissTokens: u.CacheMissTokens,
	}
}

// streamSinkAdapter 将 llm.StreamSink 适配为 pkgeino.StreamSink。
type streamSinkAdapter struct {
	sink       StreamSink
	contentBuf *strings.Builder
}

func (a *streamSinkAdapter) EmitMessageDelta(ctx context.Context, _ string, content string) error {
	if a.contentBuf != nil {
		a.contentBuf.WriteString(content)
	}
	if a.sink == nil {
		return nil
	}
	return a.sink.EmitMessageDelta(ctx, content)
}

func (a *streamSinkAdapter) EmitReasoningDelta(ctx context.Context, _ string, content string) error {
	if a.sink == nil {
		return nil
	}
	return a.sink.EmitReasoningDelta(ctx, content)
}

func (c *CallerEino) CallRaw(ctx context.Context, orgID uint, cfg *ModelConfig, body []byte) (*CallResult, error) {
	return nil, fmt.Errorf("CallerEino does not support CallRaw, use CallerHTTP")
}

func (c *CallerEino) StreamRaw(ctx context.Context, orgID uint, cfg *ModelConfig, body []byte, rawChunkSink RawChunkSink) (*CallResult, error) {
	return nil, fmt.Errorf("CallerEino does not support StreamRaw, use CallerHTTP")
}
