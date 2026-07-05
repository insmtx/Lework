// Package native implements the built-in Eino-backed Leros runtime.
package native

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	chatModel, err := pkgeino.NewChatModel(ctx, &pkgeino.ChatModelConfig{
		Provider: req.Model.Provider,
		APIKey:   req.Model.APIKey,
		Model:    req.Model.Model,
		BaseURL:  req.Model.BaseURL,
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
