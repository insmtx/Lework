package lifecycle

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/agent/leros"
	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
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
		base = leros.DefaultSystemPrompt()
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
	logs.InfoContextf(ctx, "Agent context prepare started: run_id=%s trace_id=%s assistant_id=%s conversation_id=%s input_type=%s input_text_len=%d messages=%d attachments=%d",
		req.RunID,
		req.TraceID,
		req.Assistant.ID,
		req.Conversation.ID,
		req.Input.Type,
		len(strings.TrimSpace(req.Input.Text)),
		len(req.Input.Messages),
		len(req.Input.Attachments),
	)

	cloned := cloneRequest(req)
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
	sections := make([]string, 0, 6)
	sectionNames := make([]string, 0, 5)
	if base := strings.TrimSpace(b.basePromptForRequest(req)); base != "" {
		sections = append(sections, base)
		sectionNames = append(sectionNames, "base")
	}
	if skills := strings.TrimSpace(b.buildSkillsContext()); skills != "" {
		sections = append(sections, skills)
		sectionNames = append(sectionNames, "skills")
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
		lines = append(lines, fmt.Sprintf("- %s: %s", role, truncateForPrompt(content, 500)))
	}
	if len(lines) == 2 {
		return ""
	}
	lines = append(lines, "</session-summary>")
	return strings.Join(lines, "\n")
}

func buildUserInput(req *agent.RequestContext) string {
	if req == nil {
		return ""
	}
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

func cloneRequest(req *agent.RequestContext) *agent.RequestContext {
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

func filterEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func truncateForPrompt(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "...(truncated)"
}
