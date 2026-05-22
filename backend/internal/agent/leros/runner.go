// Package leros implements the built-in Eino-backed Leros runtime.
package leros

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/agent"
	einoadapter "github.com/insmtx/Leros/backend/internal/agent/eino"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/deps"
	"github.com/insmtx/Leros/backend/internal/agent/runtime/events"
	runtimetodo "github.com/insmtx/Leros/backend/internal/agent/runtime/todo"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/insmtx/Leros/backend/tools"
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
}

// DefaultSystemPrompt 返回 Leros 内置 Agent 的基础系统提示词。
func DefaultSystemPrompt() string {
	return prompts.Get(prompts.KeyAgentSystemDefault)
}

// Runner 是 Leros 内置 Eino 运行时入口。
type Runner struct {
	toolAdapter  *einoadapter.ToolAdapter
	systemPrompt string
}

// NewRunner 创建基于 Eino Flow 的 Leros 内置 Agent。
func NewRunner(ctx context.Context, env *deps.Container) (*Runner, error) {
	if env == nil {
		return nil, fmt.Errorf("runtime dependencies are required")
	}
	if env.ToolRegistry() == nil {
		return nil, fmt.Errorf("tool registry is required")
	}

	return &Runner{
		toolAdapter:  einoadapter.NewToolAdapter(env.ToolRegistry()),
		systemPrompt: prompts.Get(prompts.KeyAgentSystemDefault),
	}, nil
}

// Run 直接执行标准化请求；统一生命周期入口应优先使用 lifecycle.Runner。
func (r *Runner) Run(ctx context.Context, req *agent.RequestContext) (*agent.RunResult, error) {
	startedAt := time.Now().UTC()
	if r == nil {
		return nil, fmt.Errorf("leros runner is not initialized")
	}

	state, err := r.buildRunState(req)
	if err != nil {
		return nil, err
	}
	return r.runWithState(ctx, state, startedAt)
}

func (r *Runner) runWithState(ctx context.Context, state *runState, startedAt time.Time) (*agent.RunResult, error) {
	req := state.req

	chatModel, err := einoadapter.NewChatModel(ctx, &config.LLMConfig{
		Provider: req.Model.Provider,
		APIKey:   req.Model.APIKey,
		Model:    req.Model.Model,
		BaseURL:  req.Model.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	einoTools, err := r.toolAdapter.EinoTools(state.toolBinding, state.eventSink)
	if err != nil {
		return nil, fmt.Errorf("build eino tools: %w", err)
	}

	// 构建对话历史消息
	historyMessages := einoadapter.BuildMessagesFromConversation(req.Conversation.Messages)

	flow, err := einoadapter.NewFlow(ctx, &einoadapter.FlowConfig{
		Model:        chatModel,
		Tools:        einoTools,
		SystemPrompt: state.systemPrompt,
		MaxStep:      state.maxStep,
		Messages:     historyMessages,
	})
	if err != nil {
		return nil, err
	}

	var message interface {
		String() string
	}
	var resultMessage string
	var usage *events.UsagePayload
	if req.EventSink != nil {
		streamedMessage, streamedUsage, streamErr := flow.StreamWithUsage(ctx, state.userInput, state.eventSink)
		err = streamErr
		if streamedMessage != nil {
			message = streamedMessage
			resultMessage = strings.TrimSpace(streamedMessage.Content)
			usage = streamedUsage
		}
	} else {
		generatedMessage, generatedUsage, generateErr := flow.GenerateWithUsage(ctx, state.userInput)
		err = generateErr
		if generatedMessage != nil {
			message = generatedMessage
			resultMessage = strings.TrimSpace(generatedMessage.Content)
			usage = generatedUsage
		}
	}
	if err != nil {
		return nil, err
	}
	if resultMessage == "" && message != nil {
		resultMessage = formatLLMResultForLog(message)
	}

	result := &agent.RunResult{
		RunID:       req.RunID,
		TraceID:     req.TraceID,
		Status:      agent.RunStatusCompleted,
		Message:     resultMessage,
		Usage:       usage,
		StartedAt:   startedAt,
		CompletedAt: time.Now().UTC(),
	}

	logs.InfoContextf(ctx, "Leros runtime final LLM result: run_id=%s actor=%s result=%s",
		req.RunID, req.Actor.UserID, formatLLMResultForLog(message))

	return result, nil
}

func (r *Runner) buildRunState(req *agent.RequestContext) (*runState, error) {
	if req == nil {
		return nil, errors.New("request context is required")
	}
	userInput := buildUserInput(req)
	if userInput == "" {
		userInput = string(req.Input.Type)
	}

	systemPrompt, err := r.buildSystemPrompt(req)
	if err != nil {
		return nil, err
	}

	eventSink := sinkForRequest(req)
	toolCtx := tools.ToolContext{
		RunID:          req.RunID,
		TraceID:        req.TraceID,
		AssistantID:    req.Assistant.ID,
		UserID:         req.Actor.UserID,
		AccountID:      req.Actor.AccountID,
		Channel:        req.Actor.Channel,
		ChatID:         req.Conversation.ID,
		ConversationID: req.Conversation.ID,
		ExternalID:     req.Actor.ExternalID,
		Metadata:       req.Metadata,
	}
	return &runState{
		req:          req,
		eventSink:    eventSink,
		userInput:    userInput,
		systemPrompt: systemPrompt,
		toolBinding: einoadapter.ToolBinding{
			ToolContext:  toolCtx,
			AllowedTools: mergeToolNames(r.availableDefaultToolNames(), req.Capability.AllowedTools),
			TodoReporter: runtimetodo.NewTracker(runtimetodo.Options{
				RunID:   req.RunID,
				TraceID: req.TraceID,
				Sink:    eventSink,
			}),
		},
		maxStep: maxStepForRequest(req),
	}, nil
}

func (r *Runner) availableDefaultToolNames() []string {
	if r == nil || r.toolAdapter == nil {
		return nil
	}
	return r.toolAdapter.AvailableToolNames(defaultToolNames)
}

func mergeToolNames(values ...[]string) []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, list := range values {
		for _, name := range list {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	return result
}

func buildUserInput(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}

	// 构建当前输入
	switch {
	case strings.TrimSpace(req.Input.Text) != "":
		return strings.TrimSpace(req.Input.Text)
	case len(req.Input.Messages) > 0:
		lines := make([]string, 0, len(req.Input.Messages))
		for _, message := range req.Input.Messages {
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			role := message.Role
			if role == "" {
				role = "user"
			}
			lines = append(lines, fmt.Sprintf("%s: %s", role, message.Content))
		}
		return strings.Join(lines, "\n")
	default:
		return string(req.Input.Type)
	}
}

func (r *Runner) buildSystemPrompt(req *agent.RequestContext) (string, error) {
	if req != nil && strings.TrimSpace(req.SystemPrompt) != "" {
		return strings.TrimSpace(req.SystemPrompt), nil
	}
	if r == nil {
		return "", nil
	}
	return strings.TrimSpace(r.systemPromptForRequest(req)), nil
}

func (r *Runner) systemPromptForRequest(req *agent.RequestContext) string {
	prompt := strings.TrimSpace(r.systemPrompt)
	if req != nil && strings.TrimSpace(req.Assistant.SystemPrompt) != "" {
		if prompt == "" {
			prompt = strings.TrimSpace(req.Assistant.SystemPrompt)
		} else {
			prompt += "\n\n" + strings.TrimSpace(req.Assistant.SystemPrompt)
		}
	}
	if req == nil {
		return prompt
	}
	return prompt
}

func maxStepForRequest(req *agent.RequestContext) int {
	if req != nil && req.Runtime.MaxStep > 0 {
		return req.Runtime.MaxStep
	}
	return 90
}

func sinkForRequest(req *agent.RequestContext) events.Sink {
	if req == nil || req.EventSink == nil {
		return events.NewNoopSink()
	}
	return req.EventSink
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
