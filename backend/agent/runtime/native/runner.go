// Package native implements the built-in Eino-backed Leros runtime.
package native

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/insmtx/Leros/backend/agent"
	runtimetodo "github.com/insmtx/Leros/backend/agent/runtime/internal/todo"
	pkgeino "github.com/insmtx/Leros/backend/pkg/eino"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/ygpkg/yg-go/logs"
)

// Runner 是 Leros 内置 Eino 运行时入口。
type Runner struct{}

// NewRunner 创建基于 Eino Flow 的 Leros 内置 Agent。
func NewRunner(context.Context) (*Runner, error) {
	return &Runner{}, nil
}

// Execute runs one prepared native request and emits only activity events.
func (r *Runner) Execute(
	ctx context.Context,
	req agent.ExecutionRequest,
	observer agent.NodeObserver,
) (agent.ExecutionResult, error) {
	if r == nil {
		return agent.ExecutionResult{}, fmt.Errorf("leros runner is not initialized")
	}
	if strings.TrimSpace(req.ExecutionID) == "" {
		return agent.ExecutionResult{}, fmt.Errorf("execution id is required")
	}
	emitter := &nodeEmitter{
		observer:    observer,
		executionID: req.ExecutionID,
		traceID:     req.TraceID,
	}
	message, usage, err := r.runWithState(ctx, req, emitter)
	if err != nil {
		return agent.ExecutionResult{}, err
	}
	return agent.ExecutionResult{
		Message: message,
		Usage:   usage,
	}, nil
}

// nodeEmitter adds execution context to strongly typed Native node events.
type nodeEmitter struct {
	observer    agent.NodeObserver
	executionID string
	traceID     string
}

func (e *nodeEmitter) emit(ctx context.Context, event agent.NodeEvent) error {
	if e == nil || e.observer == nil {
		return nil
	}
	event.ExecutionID = e.executionID
	event.TraceID = e.traceID
	return e.observer.Observe(ctx, event)
}

func (e *nodeEmitter) emitMessageDelta(ctx context.Context, messageID string, content string) error {
	return e.emit(ctx, agent.NewMessageUpdateEvent(messageID, content))
}

func (e *nodeEmitter) emitReasoningDelta(ctx context.Context, messageID string, content string) error {
	return e.emit(ctx, agent.NewReasoningUpdateEvent(messageID, content))
}

func (e *nodeEmitter) emitToolCallStarted(ctx context.Context, toolCallID string, name string, arguments string) error {
	return e.emit(ctx, agent.NewToolExecutionStartEvent(toolCallID, name, json.RawMessage(arguments)))
}

func (e *nodeEmitter) emitToolCallCompleted(ctx context.Context, toolCallID string, name string, result string, elapsedMS int64) error {
	return e.emit(ctx, agent.NewToolExecutionEndEvent(toolCallID, name, agent.MarshalRawJSON(result), elapsedMS))
}

func (e *nodeEmitter) emitToolCallFailed(ctx context.Context, toolCallID string, name string, detail string, elapsedMS int64) error {
	return e.emit(ctx, agent.NewToolExecutionEndErrorEvent(toolCallID, name, detail, elapsedMS))
}

// einoStreamSink adapts Native node emission to pkgeino.StreamSink.
type einoStreamSink struct {
	emitter *nodeEmitter
}

func (s einoStreamSink) EmitMessageDelta(ctx context.Context, messageID string, content string) error {
	return s.emitter.emitMessageDelta(ctx, messageID, content)
}

func (s einoStreamSink) EmitReasoningDelta(ctx context.Context, messageID string, content string) error {
	return s.emitter.emitReasoningDelta(ctx, messageID, content)
}

func (r *Runner) runWithState(ctx context.Context, req agent.ExecutionRequest, emitter *nodeEmitter) (string, *agent.Usage, error) {
	// 按模型上下文窗口对输入做预算截断，避免长对话累计超过 ContextLimit 触发上游拒绝。
	applyInputBudget(&req)

	chatModel, err := pkgeino.NewChatModel(ctx, &pkgeino.ChatModelConfig{
		Provider:         req.Model.Provider,
		APIKey:           req.Model.APIKey,
		Model:            req.Model.Model,
		BaseURL:          req.Model.BaseURL,
		MaxTokens:        req.Model.MaxTokens,
		Temperature:      float32Ptr(req.Model.Temperature),
		TopP:             float32PtrIf(req.Model.TopP),
		FrequencyPenalty: float32PtrIf(req.Model.FrequencyPenalty),
		PresencePenalty:  float32PtrIf(req.Model.PresencePenalty),
	})
	if err != nil {
		return "", nil, err
	}

	systemPrompt := r.buildSystemPrompt(req)

	binding := r.buildToolBinding(req, emitter)
	toolSpecs, toolInvoker, err := buildRuntimeTools(binding, emitter)
	if err != nil {
		return "", nil, fmt.Errorf("build eino tools: %w", err)
	}
	einoBaseTools := buildEinoTools(toolSpecs, toolInvoker)

	historyMessages := buildHistoryMessages(req.Messages, 20)

	flow, err := pkgeino.NewFlow(ctx, &pkgeino.FlowConfig{
		Model:        chatModel,
		Tools:        einoBaseTools,
		SystemPrompt: systemPrompt,
		Messages:     historyMessages,
	})
	if err != nil {
		return "", nil, err
	}

	var message interface {
		String() string
	}
	var resultMessage string
	var usage *agent.Usage
	if emitter != nil && emitter.observer != nil {
		streamedMessage, streamedUsage, streamErr := flow.StreamWithUsage(ctx, req.Prompt, einoStreamSink{emitter: emitter})
		err = streamErr
		if streamedMessage != nil {
			message = streamedMessage
			resultMessage = strings.TrimSpace(streamedMessage.Content)
			usage = runtimeUsagePayload(streamedUsage)
		}
	} else {
		generatedMessage, generatedUsage, generateErr := flow.GenerateWithUsage(ctx, req.Prompt)
		err = generateErr
		if generatedMessage != nil {
			message = generatedMessage
			resultMessage = strings.TrimSpace(generatedMessage.Content)
			usage = runtimeUsagePayload(generatedUsage)
		}
	}
	if err != nil {
		return "", nil, err
	}
	if resultMessage == "" && message != nil {
		resultMessage = formatLLMResultForLog(message)
	}

	logs.InfoContextf(ctx, "Leros runtime final LLM result: run_id=%s result=%s",
		req.ExecutionID, formatLLMResultForLog(message))

	return resultMessage, usage, nil
}

// buildHistoryMessages converts prepared execution messages into Eino ADK history.
func buildHistoryMessages(messages []agent.Message, maxMessages int) []adk.Message {
	if len(messages) == 0 {
		return nil
	}

	einoMessages := make([]pkgeino.Message, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		einoMessages = append(einoMessages, pkgeino.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	if maxMessages > 0 && len(einoMessages) > maxMessages {
		einoMessages = einoMessages[len(einoMessages)-maxMessages:]
	}

	return pkgeino.BuildMessages(einoMessages)
}

func (r *Runner) buildToolBinding(req agent.ExecutionRequest, emitter *nodeEmitter) toolBinding {
	return toolBinding{
		Tools:        append([]agent.Tool(nil), req.Tools...),
		AllowedTools: append([]string(nil), req.Policy.AllowedTools...),
		TodoReporter: runtimetodo.NewTracker(runtimetodo.Options{
			RunID:    req.ExecutionID,
			Observer: emitter.observer,
		}),
	}
}

func (r *Runner) buildSystemPrompt(req agent.ExecutionRequest) string {
	prompt := req.SystemPrompt
	if hint := strings.TrimSpace(prompts.Get(prompts.KeyAgentNativeSkillUsageHint)); hint != "" {
		prompt += "\n\n" + hint
	}
	return prompt
}

// float32Ptr 将温度等标量转为 *float32；v<=0（未配置）时返回 nil，交给 provider 使用默认值。
func float32Ptr(v float64) *float32 {
	if v <= 0 {
		return nil
	}
	f := float32(v)
	return &f
}

// float32PtrIf 将可选的 *float64 采样参数转为 *float32，保持 nil 语义。
func float32PtrIf(v *float64) *float32 {
	if v == nil {
		return nil
	}
	f := float32(*v)
	return &f
}

// inputBudgetRatio 输入 token 预算占整体上下文窗口的比例，余量留给推理输出，避免触发上游拒绝。
const inputBudgetRatio = 0.7

// estimateTokenCount 粗略估算文本 token 数：按 utf8 字符数除以 4（保守，宁可多估避免超限），
// 至少计为 1。
func estimateTokenCount(s string) int {
	if s == "" {
		return 0
	}
	n := utf8.RuneCountInString(s)
	if n <= 0 {
		return 0
	}
	return n/4 + 1
}

// truncateByChars 将字符串按字符数截断到 maxChars，超出时仅保留头部。maxChars<=0 时返回空串。
func truncateByChars(s string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxChars {
		return s
	}
	runes := []rune(s)
	if maxChars >= len(runes) {
		return s
	}
	return string(runes[:maxChars])
}

// applyInputBudget 根据模型上下文 window（ContextLimit，token）对历史消息与当前 prompt 做字符级 token
// 预算截断：优先保留最近消息并丢弃最旧消息，直至估算总量不超过预算；仍超预算的单条内容做字符级截断。
// ContextLimit<=0 表示未配置，直接返回保持原行为。
func applyInputBudget(req *agent.ExecutionRequest) {
	if req == nil || req.Model.ContextLimit <= 0 {
		return
	}

	budget := int(float64(req.Model.ContextLimit) * inputBudgetRatio)
	if budget <= 0 {
		return
	}

	curPrompt := req.Prompt
	promptTok := estimateTokenCount(curPrompt)
	if promptTok > budget {
		// 单条 prompt 已超过整体预算：按 budget 字符量截断 prompt，余量完全留给 prompt。
		charCap := budget * 4
		req.Prompt = truncateByChars(curPrompt, charCap)
		req.Messages = nil
		return
	}

	remain := budget - promptTok
	if remain <= 0 {
		req.Messages = nil
		return
	}

	// 从最近到最旧累加消息 token，保留累计不超 remain 的最近消息；超出的旧消息丢弃。
	kept := make([]agent.Message, 0, len(req.Messages))
	used := 0
	for i := len(req.Messages) - 1; i >= 0; i-- {
		msg := req.Messages[i]
		n := estimateTokenCount(msg.Content)
		if used+n > remain {
			// 本条加入即超预算：若已保留任何消息则丢弃本条（保持消息完整），
			// 否则本条是唯一且超预算，按字符截断其内容。
			if used == 0 {
				charCap := remain * 4
				msg.Content = truncateByChars(msg.Content, charCap)
				kept = append(kept, msg)
			}
			break
		}
		kept = append(kept, msg)
		used += n
	}

	// 反转回时间正序。
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	req.Messages = kept
}

func formatLLMResultForLog(message interface{ String() string }) string {
	if message == nil {
		return "<nil>"
	}

	formatted := strings.TrimSpace(message.String())
	if formatted == "" {
		return "<empty>"
	}
	if len(formatted) > 2000 {
		return formatted[:2000] + "...(truncated)"
	}
	return formatted
}

func runtimeUsagePayload(usage *pkgeino.Usage) *agent.Usage {
	if usage == nil {
		return nil
	}
	return agent.EnsureUsage(&agent.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
	})
}

func buildEinoTools(specs []pkgeino.ToolSpec, invoker pkgeino.ToolInvoker) []einotool.BaseTool {
	if len(specs) == 0 {
		return nil
	}
	result := make([]einotool.BaseTool, 0, len(specs))
	for _, spec := range specs {
		result = append(result, pkgeino.NewTool(spec, invoker))
	}
	return result
}
