// Package native implements the built-in Eino-backed Leros runtime.
package native

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/insmtx/Leros/backend/engines"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/runtime/deps"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/internal/runtime/todo"
	pkgeino "github.com/insmtx/Leros/backend/pkg/eino"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/insmtx/Leros/backend/tools"
	artifactdeclare "github.com/insmtx/Leros/backend/tools/artifact_declare"
	memorytools "github.com/insmtx/Leros/backend/tools/memory"
	nodetools "github.com/insmtx/Leros/backend/tools/node"
	skillmanagetools "github.com/insmtx/Leros/backend/tools/skill_manage"
	skillusetools "github.com/insmtx/Leros/backend/tools/skill_use"
	todotools "github.com/insmtx/Leros/backend/tools/todo"
	"github.com/ygpkg/yg-go/logs"
)

var defaultToolNames = []string{
	memorytools.ToolNameMemory,
	skillusetools.ToolNameSkillUse,
	skillmanagetools.ToolNameSkillManage,
	todotools.ToolNameTodo,
	nodetools.ToolNameNodeShell,
	nodetools.ToolNameNodeFileRead,
	nodetools.ToolNameNodeFileWrite,
	artifactdeclare.ToolNameArtifactDeclare,
}

// Runner 是 Leros 内置 Eino 运行时入口。
type Runner struct {
	toolAdapter *toolAdapter
}

// NewRunner 创建基于 Eino Flow 的 Leros 内置 Agent。
func NewRunner(ctx context.Context, env *deps.Container) (*Runner, error) {
	if env == nil {
		return nil, fmt.Errorf("runtime dependencies are required")
	}
	if env.ToolRegistry() == nil {
		return nil, fmt.Errorf("tool registry is required")
	}

	registry := env.ToolRegistry()
	if err := registry.Register(artifactdeclare.NewTool()); err != nil {
		return nil, fmt.Errorf("register artifact_declare tool: %w", err)
	}

	return &Runner{
		toolAdapter: newToolAdapter(env.ToolRegistry()),
	}, nil
}

// Run 执行标准化请求，通过 Event channel 支持流式输出。
func (r *Runner) Run(ctx context.Context, req engines.RunRequest) (<-chan events.Event, error) {
	if r == nil {
		return nil, fmt.Errorf("leros runner is not initialized")
	}

	eventsCh := make(chan events.Event, 256)

	go func() {
		defer close(eventsCh)
		r.executeStreaming(ctx, req, eventsCh)
	}()

	return eventsCh, nil
}

// executeStreaming 在 goroutine 中执行推理，将全部事件写入 channel。
func (r *Runner) executeStreaming(ctx context.Context, req engines.RunRequest, eventsCh chan<- events.Event) {
	channelSink := events.SinkFunc(func(ctx context.Context, event *events.Event) error {
		if event != nil {
			select {
			case eventsCh <- *event:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	sendEvent(eventsCh, events.EventStarted, req.ExecutionID)

	message, usage, err := r.runWithState(ctx, req, channelSink)
	if err != nil {
		sendEvent(eventsCh, events.EventFailed, fmt.Sprintf("run_id=%s error=%s", req.ExecutionID, err.Error()))
		return
	}

	payload, _ := json.Marshal(events.MessageResultPayload{
		Message: message,
		Usage:   usage,
	})
	eventsCh <- events.Event{
		Type:    events.EventResult,
		Payload: events.RawPayload(payload),
		Content: message,
	}

	sendEvent(eventsCh, events.EventCompleted, req.ExecutionID)
}

func sendEvent(eventsCh chan<- events.Event, eventType events.EventType, content string) {
	select {
	case eventsCh <- events.Event{Type: eventType, Content: content}:
	default:
	}
}

func (r *Runner) runWithState(ctx context.Context, req engines.RunRequest, sink events.Sink) (string, *events.UsagePayload, error) {
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
	toolSpecs, toolInvoker, err := r.toolAdapter.EinoTools(binding, sink)
	if err != nil {
		return "", nil, fmt.Errorf("build eino tools: %w", err)
	}
	einoBaseTools := buildEinoTools(toolSpecs, toolInvoker)

	// Load persisted session messages as conversation history.
	var historyMessages []adk.Message
	if req.SessionID != "" {
		msgList, loadErr := loadSessionContext(ctx, req.SessionID, 1, 20)
		if loadErr != nil {
			return "", nil, fmt.Errorf("load session context: %w", loadErr)
		}
		if msgList != nil && len(msgList.Items) > 0 {
			historyMessages = buildHistoryMessages(msgList.Items, 20)
		}
	}

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
	var usage *events.UsagePayload
	if sink != nil {
		streamedMessage, streamedUsage, streamErr := flow.StreamWithUsage(ctx, req.Prompt, streamSink{sink: sink})
		err = streamErr
		if streamedMessage != nil {
			message = streamedMessage
			resultMessage = strings.TrimSpace(streamedMessage.Content)
			usage = usagePayload(streamedUsage)
		}
	} else {
		generatedMessage, generatedUsage, generateErr := flow.GenerateWithUsage(ctx, req.Prompt)
		err = generateErr
		if generatedMessage != nil {
			message = generatedMessage
			resultMessage = strings.TrimSpace(generatedMessage.Content)
			usage = usagePayload(generatedUsage)
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

// buildHistoryMessages converts persisted session messages into Eino ADK
// history messages. It sorts by sequence, skips empty content, and limits
// to the most recent maxMessages entries.
func buildHistoryMessages(messages []contract.SessionMessage, maxMessages int) []adk.Message {
	if len(messages) == 0 {
		return nil
	}

	// Sort by sequence number ascending.
	sorted := make([]contract.SessionMessage, len(messages))
	copy(sorted, messages)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Sequence < sorted[j].Sequence
	})

	// Filter empty and build eino.Message slice.
	einoMessages := make([]pkgeino.Message, 0, len(sorted))
	for _, msg := range sorted {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		einoMessages = append(einoMessages, pkgeino.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Limit to most recent maxMessages.
	if maxMessages > 0 && len(einoMessages) > maxMessages {
		einoMessages = einoMessages[len(einoMessages)-maxMessages:]
	}

	return pkgeino.BuildMessages(einoMessages)
}

func (r *Runner) buildToolBinding(req engines.RunRequest, sink events.Sink) toolBinding {
	workDir := strings.TrimSpace(req.WorkDir)
	toolCtx := tools.ToolContext{
		RunID:          req.ExecutionID,
		TraceID:        req.SessionID,
		ConversationID: req.SessionID,
		WorkDir:        workDir,
	}
	return toolBinding{
		ToolContext:  toolCtx,
		AllowedTools: r.availableDefaultToolNames(),
		TodoReporter: runtimetodo.NewTracker(runtimetodo.Options{
			RunID: req.ExecutionID,
			Sink:  sink,
		}),
	}
}

func (r *Runner) availableDefaultToolNames() []string {
	if r == nil || r.toolAdapter == nil {
		return nil
	}
	return r.toolAdapter.AvailableToolNames(defaultToolNames)
}

// buildSystemPrompt 从请求构建最终 system prompt，末尾追加 skill 调用提示。
func (r *Runner) buildSystemPrompt(req engines.RunRequest) string {
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

type streamSink struct {
	sink events.Sink
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

func usagePayload(usage *pkgeino.Usage) *events.UsagePayload {
	if usage == nil {
		return nil
	}
	return &events.UsagePayload{
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
