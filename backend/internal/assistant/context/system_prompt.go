package assistantcontext

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/ygpkg/yg-go/logs"
)

// buildSkillLoadingContext 构建 Skill 加载指令 + available_skills 数据。
// 静态提示词模板来自 prompts 包，动态 skills 数据在运行时从文件系统扫描注入。
func (b *ContextBuilder) buildSkillLoadingContext(ctx context.Context) string {
	var skillsData string
	summaries, err := skillcatalog.List()
	if err == nil && len(summaries) > 0 {
		var sb strings.Builder
		sb.WriteString("\n")
		for _, s := range summaries {
			sb.WriteString("- ")
			sb.WriteString(s.Name)
			sb.WriteString(": ")
			sb.WriteString(s.Description)
			sb.WriteString("\n")
		}
		skillsData = strings.TrimSpace(sb.String())
	}

	template := prompts.Get(prompts.KeyAgentNativeSkillLoading)
	return strings.Replace(template, "{skills_data}", skillsData, 1)
}

// BuildSystemPrompt 生成统一系统提示词，供所有运行时复用。
// 组装顺序：
//  1. 角色定义（Leros 助手身份 + Assistant 级 SystemPrompt）
//  2. 任务完成（反编造、持续执行直到产出真实结果）
//  3. 工具强制（执行纪律、必须用工具而非描述意图）
//  4. Skill 使用指导（加载指令 + 保存/patch 指令 + <available_skills>）
//  5. Memory 工具使用指导
//  6. 项目工作区信息（可见性规则 + 运行系统环境）
//  7. 产物声明（必须声明交付物，否则用户不可见）
//  8. 持久化记忆注入（USER.md + MEMORY.md）
//  9. 运行元信息（日期 / SessionID / Model）
//
// 10. 平台格式指导（按 Channel）
func (b *ContextBuilder) BuildSystemPrompt(ctx context.Context, req *assistantdomain.RunRequest) (string, error) {
	sections := make([]string, 0, 10)
	sectionNames := make([]string, 0, 10)

	// 角色定义：Leros 助手身份声明 + Assistant 级自定义 SystemPrompt
	identity := strings.TrimSpace(prompts.Get(prompts.KeyAgentSystemDefault))
	if req != nil && strings.TrimSpace(req.Assistant.SystemPrompt) != "" {
		identity += "\n\n" + strings.TrimSpace(req.Assistant.SystemPrompt)
	}
	if identity != "" {
		sections = append(sections, identity)
		sectionNames = append(sectionNames, "identity")
	}

	// 任务完成：反编造、持续执行直到产出真实结果
	if taskCompletion := strings.TrimSpace(prompts.Get(prompts.KeyAgentNativeTaskCompletion)); taskCompletion != "" {
		sections = append(sections, taskCompletion)
		sectionNames = append(sectionNames, "task_completion")
	}

	// 工具强制：执行纪律、必须用工具而非描述意图
	if toolEnforce := strings.TrimSpace(prompts.Get(prompts.KeyAgentNativeToolEnforcement)); toolEnforce != "" {
		sections = append(sections, toolEnforce)
		sectionNames = append(sectionNames, "tool_enforcement")
	}

	// Skill 加载指令 + available_skills
	if skillLoading := strings.TrimSpace(b.buildSkillLoadingContext(ctx)); skillLoading != "" {
		sections = append(sections, skillLoading)
		sectionNames = append(sectionNames, "skill_loading")
	}

	// Memory 工具使用指导
	if memGuidance := strings.TrimSpace(prompts.Get(prompts.KeyAgentSystemMemoryGuidance)); memGuidance != "" {
		sections = append(sections, memGuidance)
		sectionNames = append(sectionNames, "memory_guidance")
	}

	// 项目工作区信息
	if workspace := strings.TrimSpace(buildWorkspaceContext(req)); workspace != "" {
		sections = append(sections, workspace)
		sectionNames = append(sectionNames, "workspace")
	}

	// 产物声明
	if artifactDecl := strings.TrimSpace(prompts.Get(prompts.KeyAgentNativeArtifactDeclaration)); artifactDecl != "" {
		sections = append(sections, artifactDecl)
		sectionNames = append(sectionNames, "artifact_declaration")
	}

	// 持久化记忆注入
	if memory := strings.TrimSpace(buildMemoryContext(ctx, b.Memory)); memory != "" {
		sections = append(sections, memory)
		sectionNames = append(sectionNames, "memory")
	}

	// 日期 / SessionID / Model
	if runMeta := strings.TrimSpace(buildRunMetaContext(req)); runMeta != "" {
		sections = append(sections, runMeta)
		sectionNames = append(sectionNames, "run_meta")
	}

	// 平台格式指导
	if platform := strings.TrimSpace(buildPlatformContext(req)); platform != "" {
		sections = append(sections, platform)
		sectionNames = append(sectionNames, "platform")
	}

	prompt := strings.Join(sections, "\n\n")
	logs.InfoContextf(ctx, "Agent system prompt built: run_id=%s trace_id=%s sections=%s section_count=%d prompt_len=%d",
		requestRunID(req), requestTraceID(req), strings.Join(sectionNames, ","), len(sections), len(prompt))
	return prompt, nil
}

// buildMemoryContext 从本地记忆存储器中拉取记忆块作为 prompt 上下文。
func buildMemoryContext(ctx context.Context, reader MemoryReader) string {
	if reader == nil {
		return ""
	}
	block, err := reader.BuildPromptBlock(ctx)
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

// buildWorkspaceContext 从请求中解析 workspace 信息并构建提示上下文。
func buildWorkspaceContext(req *assistantdomain.RunRequest) string {
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
- 会话日志目录: %s

**工作区可见性规则：**
- 仅项目工作目录下的内容对用户可见，可被用户访问和下载。
- 不需要让用户看见的临时文件、中间产物，应在临时目录中创建。
- 会话日志目录用于存放运行日志，用户无法直接访问。

**运行系统环境：**
- Host: %s`, plan.RepoDir, plan.TurnTmpDir, plan.TurnLogDir, runtime.GOOS)
}

// buildRunMetaContext 返回运行元信息（Layer 9）：当前日期、会话ID、模型。
func buildRunMetaContext(req *assistantdomain.RunRequest) string {
	if req == nil {
		return ""
	}
	now := time.Now()
	dateStr := now.Format("2006-01-02 (Monday)")
	parts := []string{
		fmt.Sprintf("- 当前日期: %s", dateStr),
	}
	if req.Conversation.ID != "" {
		parts = append(parts, fmt.Sprintf("- 会话ID: %s", req.Conversation.ID))
	}
	if req.Model.Model != "" {
		modelLabel := req.Model.Model
		if req.Model.Provider != "" {
			modelLabel = req.Model.Provider + "/" + modelLabel
		}
		parts = append(parts, fmt.Sprintf("- 模型: %s", modelLabel))
	}
	return "## 运行信息\n" + strings.Join(parts, "\n")
}

// platformPromptKeys 将 Channel 映射到 prompts 包中注册的 key。
var platformPromptKeys = map[string]string{
	"wechat": prompts.KeyAgentSystemPlatformWechat,
	"feishu": prompts.KeyAgentSystemPlatformFeishu,
	"slack":  prompts.KeyAgentSystemPlatformSlack,
	"api":    prompts.KeyAgentSystemPlatformAPI,
}

// buildPlatformContext 根据 Channel 返回平台格式指导（Layer 10）。
func buildPlatformContext(req *assistantdomain.RunRequest) string {
	if req == nil || req.Actor.Channel == "" {
		return ""
	}
	key, ok := platformPromptKeys[req.Actor.Channel]
	if !ok {
		return ""
	}
	return strings.TrimSpace(prompts.Get(key))
}

// requestRunID 安全取出 Request 中的 RunID，nil 时返回空。
func requestRunID(req *assistantdomain.RunRequest) string {
	if req == nil {
		return ""
	}
	return req.RunID
}

// requestTraceID 安全取出 Request 中的 TraceID，nil 时返回空。
func requestTraceID(req *assistantdomain.RunRequest) string {
	if req == nil {
		return ""
	}
	return req.TraceID
}
