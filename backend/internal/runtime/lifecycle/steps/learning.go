package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
	lifecyclecontext "github.com/insmtx/Leros/backend/internal/runtime/lifecycle/context"
	lifecyclejournal "github.com/insmtx/Leros/backend/internal/runtime/lifecycle/journal"
	"github.com/ygpkg/yg-go/logs"
)

const (
	ToolNameMemory      = "memory"
	toolNameSkillManage = "skill_manage"
)

const learningCheckPrompt = `System learning check:
The main task is complete. Decide whether any long-term useful information should be saved.
Save stable user preferences, project facts, reusable workflows, or tool friction. Do not save transient task progress or one-off results.
If nothing is worth saving, reply that no memory update is needed.`

const memoryFlushPrompt = `System memory flush:
The current conversation is about to be compacted or reset. Save only information that will remain useful later, such as user preferences, project facts, stable workflows, or tool friction. Do not save transient task progress or one-off results.`

type ToolAvailability interface {
	AvailableToolNames(names []string) []string
}

type LearningService struct {
	Builder          ContextBuilder
	Delegate         agent.Runner
	ToolAvailability ToolAvailability
}

// AfterRunLearning 在主任务完成后触发学习检查，让 agent 判断是否需要沉淀长期记忆或技能流程。
func (s *LearningService) AfterRunLearning(ctx context.Context, req *agent.RequestContext, result *agent.RunResult, trace *lifecyclejournal.RunTrace) error {
	if s == nil || s.Builder == nil || s.Delegate == nil || req == nil || result == nil || !ShouldRunLearningCheck(req, result, trace) {
		return nil
	}

	allowedTools := availableToolNames(s.ToolAvailability, []string{ToolNameMemory, toolNameSkillManage})
	if len(allowedTools) == 0 {
		return nil
	}

	learningReq := lifecyclecontext.CloneRequest(req)
	learningReq.Input = agent.InputContext{
		Type: agent.InputTypeTaskInstruction,
		Messages: []agent.InputMessage{{
			Role:    "user",
			Content: buildLearningPrompt(req, result, trace),
		}},
	}
	learningReq.Capability.AllowedTools = allowedTools
	learningReq.Runtime.MaxStep = 3
	learningReq.EventSink = events.NewNoopSink()

	next, err := s.Builder.Prepare(ctx, learningReq)
	if err != nil {
		return err
	}
	next.EventSink = events.NewNoopSink()
	_, err = s.Delegate.Run(ctx, next)
	return err
}

// BeforeCompact 在上下文压缩前尝试刷新长期记忆，避免稳定偏好或项目事实随上下文丢失。
func (s *LearningService) BeforeCompact(ctx context.Context, req *agent.RequestContext) error {
	return s.runMemoryFlush(ctx, req, "compact")
}

// BeforeReset 在会话重置前尝试刷新长期记忆，保留后续会话仍有价值的信息。
func (s *LearningService) BeforeReset(ctx context.Context, req *agent.RequestContext) error {
	return s.runMemoryFlush(ctx, req, "reset")
}

// runMemoryFlush 构造一次只允许调用 memory 工具的内部请求，用于在生命周期边界保存稳定信息。
func (s *LearningService) runMemoryFlush(ctx context.Context, req *agent.RequestContext, reason string) error {
	if s == nil || s.Builder == nil || s.Delegate == nil || req == nil {
		return nil
	}
	allowedTools := availableToolNames(s.ToolAvailability, []string{ToolNameMemory})
	if len(allowedTools) == 0 {
		return nil
	}

	flushReq := lifecyclecontext.CloneRequest(req)
	flushReq.Input = agent.InputContext{
		Type: agent.InputTypeTaskInstruction,
		Messages: []agent.InputMessage{{
			Role:    "user",
			Content: memoryFlushPrompt + "\n\nReason: " + strings.TrimSpace(reason),
		}},
	}
	flushReq.Capability.AllowedTools = allowedTools
	flushReq.Runtime.MaxStep = 2
	flushReq.EventSink = events.NewNoopSink()

	prepared, err := s.Builder.Prepare(ctx, flushReq)
	if err != nil {
		return err
	}
	prepared.EventSink = events.NewNoopSink()
	_, err = s.Delegate.Run(ctx, prepared)
	return err
}

type LearningStep struct {
	Service *LearningService
}

func (LearningStep) Name() string {
	return "learning"
}

// Run 执行生命周期学习步骤；学习失败只记录告警，不阻断主任务返回。
func (s LearningStep) Run(ctx context.Context, state *State) error {
	if s.Service == nil || state == nil || state.Err != nil {
		return nil
	}
	if err := s.Service.AfterRunLearning(ctx, state.Request, state.Result, state.Journal.Trace()); err != nil {
		logs.WarnContextf(ctx, "Leros lifecycle learning check failed: %v", err)
	}
	return nil
}

func availableToolNames(availability ToolAvailability, names []string) []string {
	if availability == nil {
		return nil
	}
	return availability.AvailableToolNames(names)
}

// ShouldRunLearningCheck 根据运行结果和 trace 判断是否值得启动一次学习检查。
func ShouldRunLearningCheck(req *agent.RequestContext, result *agent.RunResult, trace *lifecyclejournal.RunTrace) bool {
	if req == nil || result == nil || result.Status != agent.RunStatusCompleted {
		return false
	}
	if trace == nil {
		trace = &lifecyclejournal.RunTrace{}
	}
	if alreadyCalledLearningTool(trace.ToolNames) {
		return false
	}
	if containsLearningCue(agent.BuildUserInput(req)) {
		return true
	}
	if trace.ToolFailures > 0 {
		return true
	}
	if trace.ToolCalls >= 5 {
		return true
	}
	if trace.UsedSkillTool && trace.ToolCalls >= 3 {
		return true
	}
	return false
}

// buildLearningPrompt 汇总输入、工具调用、过程 trace 和最终回答，交给 agent 判断是否需要写记忆。
func buildLearningPrompt(req *agent.RequestContext, result *agent.RunResult, trace *lifecyclejournal.RunTrace) string {
	if trace == nil {
		trace = &lifecyclejournal.RunTrace{}
	}
	var builder strings.Builder
	builder.WriteString(learningCheckPrompt)
	builder.WriteString("\n\nRun summary:")
	if req != nil {
		if req.Input.Type != "" {
			builder.WriteString("\n- input_type: ")
			builder.WriteString(string(req.Input.Type))
		}
		if req.Actor.UserID != "" {
			builder.WriteString("\n- actor: ")
			builder.WriteString(req.Actor.UserID)
		}
	}
	builder.WriteString(fmt.Sprintf("\n- status: %s", result.Status))
	builder.WriteString(fmt.Sprintf("\n- tool_calls: %d", trace.ToolCalls))
	builder.WriteString(fmt.Sprintf("\n- tool_failures: %d", trace.ToolFailures))
	if len(trace.ToolNames) > 0 {
		builder.WriteString("\n- tools: ")
		builder.WriteString(strings.Join(uniqueStrings(trace.ToolNames), ", "))
	}
	if hasToolEvents(trace.Events) {
		builder.WriteString("\n- tool_trace: ")
		builder.WriteString(lifecyclecontext.TruncateForPrompt(formatToolTrace(trace.Events), 1200))
	}
	if len(trace.Events) > 0 {
		builder.WriteString("\n- process_trace: ")
		builder.WriteString(lifecyclecontext.TruncateForPrompt(formatProcessTrace(trace.Events), 1200))
	}
	if strings.TrimSpace(result.Message) != "" {
		builder.WriteString("\n- final_answer: ")
		builder.WriteString(lifecyclecontext.TruncateForPrompt(result.Message, 1200))
	}
	return builder.String()
}

// alreadyCalledLearningTool 避免主任务已经调用过学习类工具时再次触发学习检查。
func alreadyCalledLearningTool(names []string) bool {
	for _, name := range names {
		switch name {
		case ToolNameMemory, toolNameSkillManage:
			return true
		}
	}
	return false
}

// containsLearningCue 识别用户输入中明确要求记住偏好或后续行为的提示。
func containsLearningCue(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	cues := []string{
		"remember", "next time", "preference", "don't do that again", "do not do that again",
	}
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// hasToolEvents 判断 trace 中是否包含工具调用事件，用于决定是否追加工具轨迹摘要。
func hasToolEvents(records []events.RunEventRecord) bool {
	for _, record := range records {
		switch record.Type {
		case events.EventToolCallStarted, events.EventToolCallCompleted, events.EventToolCallFailed:
			return true
		}
	}
	return false
}

// formatToolTrace 将工具调用事件压缩成 name(status) 形式，减少学习提示词长度。
func formatToolTrace(records []events.RunEventRecord) string {
	if len(records) == 0 {
		return ""
	}
	parts := make([]string, 0, len(records))
	for _, record := range records {
		status := toolEventStatus(record.Type)
		name := toolNameFromEventRecord(record)
		if name == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", name, status))
	}
	return strings.Join(parts, ", ")
}

// toolEventStatus 把生命周期事件类型映射为学习提示词中更短的工具状态。
func toolEventStatus(eventType events.EventType) string {
	switch eventType {
	case events.EventToolCallFailed:
		return "error"
	case events.EventToolCallCompleted:
		return "ok"
	default:
		return "started"
	}
}

// formatProcessTrace 提取消息、推理、结果和工具事件的关键信息，形成可读的过程摘要。
func formatProcessTrace(records []events.RunEventRecord) string {
	if len(records) == 0 {
		return ""
	}
	parts := make([]string, 0, len(records))
	for _, record := range records {
		switch record.Type {
		case events.EventMessageDelta, events.EventReasoningDelta, events.EventResult:
			content := strings.TrimSpace(contentFromEventRecord(record))
			if content == "" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s:%s", record.Type, lifecyclecontext.TruncateForPrompt(content, 160)))
		case events.EventToolCallStarted, events.EventToolCallCompleted, events.EventToolCallFailed:
			if name := toolNameFromEventRecord(record); name != "" {
				parts = append(parts, fmt.Sprintf("%s:%s", record.Type, name))
			}
		default:
			parts = append(parts, string(record.Type))
		}
	}
	return strings.Join(parts, " | ")
}

// toolNameFromEventRecord 复用 journal 的解析逻辑，从事件 payload 中取得工具名称。
func toolNameFromEventRecord(record events.RunEventRecord) string {
	event := &events.Event{
		Type:    record.Type,
		Payload: record.Payload,
	}
	return lifecyclejournal.ToolNameFromEvent(event)
}

// contentFromEventRecord 从可展示的事件 payload 中提取文本内容，解析失败时返回空字符串。
func contentFromEventRecord(record events.RunEventRecord) string {
	switch record.Type {
	case events.EventMessageDelta, events.EventReasoningDelta:
		payload, err := events.DecodePayload[events.MessageDeltaPayload](&events.Event{Type: record.Type, Payload: record.Payload})
		if err == nil {
			return payload.Content
		}
	case events.EventResult:
		payload, err := events.DecodePayload[events.RunResultPayload](&events.Event{Type: record.Type, Payload: record.Payload})
		if err == nil {
			return payload.Message
		}
	}
	return ""
}
