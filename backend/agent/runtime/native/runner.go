// Package native implements the built-in Eino-backed Leros runtime.
package native

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/agent/runtime/todo"
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
	observer agent.Observer,
) (agent.ExecutionResult, error) {
	if r == nil {
		return agent.ExecutionResult{}, fmt.Errorf("leros runner is not initialized")
	}
	if strings.TrimSpace(req.ExecutionID) == "" {
		return agent.ExecutionResult{}, fmt.Errorf("execution id is required")
	}
	var sink activitySink = events.NewNoopSink()
	if observer != nil {
		sink = observer
	}
	message, usage, err := r.runWithState(ctx, req, sink)
	if err != nil {
		return agent.ExecutionResult{}, err
	}
	return agent.ExecutionResult{
		Message: message,
		Usage:   usage,
	}, nil
}

// activitySink is the internal observer contract used by the native runtime.
type activitySink interface {
	Emit(ctx context.Context, event *agent.Event) error
}

// streamSink adapts activitySink to the Eino streaming sink protocol.
type streamSink struct {
	sink activitySink
}

func (s streamSink) EmitMessageDelta(ctx context.Context, messageID string, content string) error {
	if s.sink == nil {
		return nil
	}
	return s.sink.Emit(ctx, events.NewMessageDelta(messageID, content))
}

func (s streamSink) EmitReasoningDelta(ctx context.Context, messageID string, content string) error {
	if s.sink == nil {
		return nil
	}
	return s.sink.Emit(ctx, events.NewReasoningDelta(messageID, content))
}

func (r *Runner) runWithState(ctx context.Context, req agent.ExecutionRequest, sink activitySink) (string, *agent.Usage, error) {
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

	binding := r.buildToolBinding(req, sink)
	toolSpecs, toolInvoker, err := buildRuntimeTools(binding, sink)
	if err != nil {
		return "", nil, fmt.Errorf("build eino tools: %w", err)
	}
	einoBaseTools := buildEinoTools(toolSpecs, toolInvoker)

	historyMessages := buildHistoryMessages(req.Messages, 20)

	flow, err := pkgeino.NewFlow(ctx, &pkgeino.FlowConfig{
		Model:        chatModel,
		Tools:        einoBaseTools,
		SystemPrompt: systemPrompt,
		MaxStep:      90,
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
	if sink != nil {
		streamedMessage, streamedUsage, streamErr := flow.StreamWithUsage(ctx, req.Prompt, streamSink{sink: sink})
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

func (r *Runner) buildToolBinding(req agent.ExecutionRequest, sink activitySink) toolBinding {
	return toolBinding{
		Tools:        append([]agent.Tool(nil), req.Tools...),
		AllowedTools: append([]string(nil), req.Policy.AllowedTools...),
		TodoReporter: runtimetodo.NewTracker(runtimetodo.Options{
			RunID: req.ExecutionID,
			Sink:  sink,
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
	return &agent.Usage{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
	}
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
