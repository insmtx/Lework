package lifecyclecontext

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	"github.com/insmtx/Leros/backend/internal/runtime/drivers/native"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/ygpkg/yg-go/logs"
)

// ContextBuilder 统一为内部 Agent 和外部 CLI 构建运行上下文。
type ContextBuilder struct {
	BaseSystemPrompt string
	Runtime          RuntimeProvider
	SessionMessages  SessionMessageProvider
}

// RuntimeProvider 聚合运行时依赖供 ContextBuilder 使用。
type RuntimeProvider interface {
	SkillsProvider() skillcatalog.CatalogProvider
}

// NewContextBuilder 创建统一上下文构建器。
func NewContextBuilder(cfg ContextBuilder) *ContextBuilder {
	base := strings.TrimSpace(cfg.BaseSystemPrompt)
	if base == "" {
		base = native.DefaultSystemPrompt()
	}
	return &ContextBuilder{
		BaseSystemPrompt: base,
		Runtime:          cfg.Runtime,
		SessionMessages:  cfg.SessionMessages,
	}
}

// SkillsProvider 返回运行时上下文中的技能提供者。
func (b *ContextBuilder) SkillsProvider() skillcatalog.CatalogProvider {
	if b == nil || b.Runtime == nil {
		return nil
	}
	return b.Runtime.SkillsProvider()
}

// Prepare 克隆请求并注入 memory、skill、session 上下文。
func (b *ContextBuilder) Prepare(ctx context.Context, req *agent.RequestContext) (*agent.RequestContext, error) {
	if req == nil {
		return nil, fmt.Errorf("request context is required")
	}

	startedAt := time.Now()
	logs.InfoContextf(ctx, "Agent context prepare started: run_id=%s trace_id=%s assistant_id=%s conversation_id=%s input_type=%s messages=%d attachments=%d",
		req.RunID,
		req.TraceID,
		req.Assistant.ID,
		req.Conversation.ID,
		req.Input.Type,
		len(req.Input.Messages),
		len(req.Input.Attachments),
	)

	cloned := CloneRequest(req)
	if b.SessionMessages != nil {
		if err := b.SessionMessages.Prepare(ctx, cloned); err != nil {
			logs.WarnContextf(ctx, "Agent session context prepare failed: run_id=%s trace_id=%s error=%v",
				req.RunID, req.TraceID, err)
			return nil, err
		}
	}
	systemPrompt, err := b.BuildSystemPrompt(ctx, cloned)
	if err != nil {
		logs.WarnContextf(ctx, "Agent context build system prompt failed: run_id=%s trace_id=%s error=%v",
			req.RunID, req.TraceID, err)
		return nil, err
	}

	cloned.SystemPrompt = systemPrompt
	logs.InfoContextf(ctx, "Agent context prepare completed: run_id=%s trace_id=%s system_prompt_len=%d elapsed=%s",
		cloned.RunID, cloned.TraceID, len(cloned.SystemPrompt), time.Since(startedAt))
	return cloned, nil
}

// BuildSystemPrompt 生成统一系统提示词，供所有运行时复用。
func (b *ContextBuilder) BuildSystemPrompt(ctx context.Context, req *agent.RequestContext) (string, error) {
	sections := make([]string, 0, 7)
	sectionNames := make([]string, 0, 6)
	if base := strings.TrimSpace(b.basePromptForRequest(req)); base != "" {
		sections = append(sections, base)
		sectionNames = append(sectionNames, "base")
	}
	if workspace := strings.TrimSpace(buildWorkspaceContext(req)); workspace != "" {
		sections = append(sections, workspace)
		sectionNames = append(sectionNames, "workspace")
	}
	if skills := strings.TrimSpace(b.buildSkillsContext()); skills != "" {
		sections = append(sections, skills)
		sectionNames = append(sectionNames, "skills")
	}
	if session := strings.TrimSpace(buildSessionSummaryContext(req)); session != "" {
		sections = append(sections, session)
		sectionNames = append(sectionNames, "session_summary")
	}
	if memory := strings.TrimSpace(buildMemoryContext(ctx)); memory != "" {
		sections = append(sections, memory)
		sectionNames = append(sectionNames, "memory")
	}
	if guidance := strings.TrimSpace(learningGuidance()); guidance != "" {
		sections = append(sections, guidance)
		sectionNames = append(sectionNames, "learning_guidance")
	}
	prompt := strings.Join(sections, "\n\n")
	logs.InfoContextf(ctx, "Agent system prompt built: run_id=%s trace_id=%s sections=%s section_count=%d prompt_len=%d",
		requestRunID(req), requestTraceID(req), strings.Join(sectionNames, ","), len(sections), len(prompt))
	return prompt, nil
}

// basePromptForRequest 取基础 system prompt，若请求中有 Assistant 级别的 prompt 则追加。
func (b *ContextBuilder) basePromptForRequest(req *agent.RequestContext) string {
	prompt := strings.TrimSpace(b.BaseSystemPrompt)
	if req != nil && strings.TrimSpace(req.Assistant.SystemPrompt) != "" {
		if prompt == "" {
			prompt = strings.TrimSpace(req.Assistant.SystemPrompt)
		} else {
			prompt += "\n\n" + strings.TrimSpace(req.Assistant.SystemPrompt)
		}
	}
	return prompt
}

// buildSkillsContext 从技能目录构建技能上下文文本，包含摘要和 always-on 技能的完整内容。
func (b *ContextBuilder) buildSkillsContext() string {
	if b == nil || b.SkillsProvider() == nil {
		logs.Debug("Agent skills context skipped: skills provider unavailable")
		return ""
	}
	catalog := b.SkillsProvider().Current()
	if catalog == nil {
		logs.Debug("Agent skills context skipped: catalog unavailable")
		return ""
	}
	summaries := catalog.List()
	if len(summaries) == 0 {
		logs.Debug("Agent skills context skipped: empty catalog")
		return ""
	}

	sections := []string{buildSkillSummarySection(summaries)}
	alwaysCount := 0
	for _, summary := range summaries {
		if !summary.Always {
			continue
		}
		entry, err := catalog.Get(summary.Name)
		if err != nil {
			logs.Warnf("Agent always-on skill load failed: skill=%s error=%v", summary.Name, err)
			continue
		}
		alwaysCount++
		sections = append(sections, "## Skill: "+entry.Manifest.Name+"\n"+strings.TrimSpace(entry.Body))
	}
	logs.Infof("Agent skills context built: skills=%d always_on=%d", len(summaries), alwaysCount)
	return strings.Join(filterEmpty(sections), "\n\n")
}

// buildSkillSummarySection 将技能摘要格式化为 prompt 中的可用技能列表。
func buildSkillSummarySection(summaries []skillcatalog.Summary) string {
	var builder strings.Builder
	builder.WriteString("Available skills:\n")
	for _, summary := range summaries {
		builder.WriteString("- ")
		builder.WriteString(summary.Name)
		builder.WriteString(": ")
		builder.WriteString(summary.Description)
		if summary.Category != "" {
			builder.WriteString(" [category=")
			builder.WriteString(summary.Category)
			builder.WriteString("]")
		}
		if len(summary.RequiresTools) > 0 {
			builder.WriteString(" [requires_tools=")
			builder.WriteString(strings.Join(summary.RequiresTools, ","))
			builder.WriteString("]")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("\nLoad a skill body only when it is relevant to the current task.")
	return strings.TrimSpace(builder.String())
}

// buildMemoryContext 从本地记忆存储器中拉取记忆块作为 prompt 上下文。
func buildMemoryContext(ctx context.Context) string {
	store, err := localmemory.NewStore(localmemory.Options{})
	if err != nil {
		logs.WarnContextf(ctx, "Agent memory context skipped: create store error=%v", err)
		return ""
	}
	block, err := store.BuildPromptBlock(ctx)
	if err != nil {
		logs.WarnContextf(ctx, "Agent memory context skipped: build prompt block error=%v", err)
		return ""
	}
	block = strings.TrimSpace(block)
	if block == "" {
		logs.DebugContextf(ctx, "Agent memory context skipped: empty prompt block")
		return ""
	}
	logs.InfoContextf(ctx, "Agent memory context built: len=%d", len(block))
	return block
}

// buildSessionSummaryContext 将最近对话记录（最多10条）压缩为 session-summary 块。
func buildSessionSummaryContext(req *agent.RequestContext) string {
	if req == nil || len(req.Conversation.Messages) == 0 {
		return ""
	}
	const maxMessages = 10
	messages := req.Conversation.Messages
	if len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}

	lines := make([]string, 0, len(messages)+2)
	lines = append(lines, "<session-summary>")
	lines = append(lines, "[System note: Recent conversation context, not new user input.]")
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", role, TruncateForPrompt(content, 500)))
	}
	if len(lines) == 2 {
		return ""
	}
	lines = append(lines, "</session-summary>")
	return strings.Join(lines, "\n")
}

// CloneRequest 深拷贝一份 RequestContext，防止 Prepare 过程污染原始请求。
func CloneRequest(req *agent.RequestContext) *agent.RequestContext {
	if req == nil {
		return nil
	}
	cloned := *req
	cloned.Assistant.Skills = append([]string{}, req.Assistant.Skills...)
	cloned.Assistant.Tools = append([]string{}, req.Assistant.Tools...)
	cloned.Conversation.Messages = append([]agent.InputMessage{}, req.Conversation.Messages...)
	cloned.Input.Messages = append([]agent.InputMessage{}, req.Input.Messages...)
	cloned.Input.Attachments = append([]agent.Attachment{}, req.Input.Attachments...)
	cloned.Capability.AllowedTools = append([]string{}, req.Capability.AllowedTools...)
	if req.Metadata != nil {
		cloned.Metadata = make(map[string]any, len(req.Metadata))
		for key, value := range req.Metadata {
			cloned.Metadata[key] = value
		}
	}
	cloned.SystemPrompt = req.SystemPrompt
	return &cloned
}

// filterEmpty 过滤切片中的空白字符串，返回所有非空值。
func filterEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// TruncateForPrompt 将字符串截断到指定长度，超长时追加截断标记。
func TruncateForPrompt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}

// learningGuidance 返回自学习规则指引，指导 Agent 何时使用 memory 和 skill_manage。
func learningGuidance() string {
	return `## Self-learning rules
- Use memory for stable user preferences, project facts, or explicit remember requests.
- Use skill_manage for reusable workflow improvements discovered during complex tasks.
- Do not save transient task progress, ordinary logs, or one-off results.`
}

// requestRunID 安全取出 Request 中的 RunID，nil 时返回空。
func requestRunID(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	return req.RunID
}

// requestTraceID 安全取出 Request 中的 TraceID，nil 时返回空。
func requestTraceID(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	return req.TraceID
}

// buildWorkspaceContext 从请求中解析 workspace 信息并构建提示上下文。
func buildWorkspaceContext(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
	plan, ok, err := agentworkspace.FromAgentRequest(req)
	if err != nil || !ok {
		return ""
	}
	return fmt.Sprintf(`## 工作区信息
- 项目工作目录: %s
- 本次请求临时目录: %s
- 会话日志目录: %s`, plan.RepoDir, plan.TurnTmpDir, plan.TurnLogDir)
}
