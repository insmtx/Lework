package agentruncontext

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/prompts"
	"github.com/ygpkg/yg-go/logs"
)

// buildSkillLoadingContext 构建 Skill 加载指令 + available_skills 数据。
func (b *ContextBuilder) buildSkillLoadingContext(ctx context.Context, req *agentrundomain.RunRequest) string {
	var skillsData string
	catalog, err := catalogForRequest(req)
	if err != nil {
		logs.WarnContextf(ctx, "resolve run skill catalog: %v", err)
	}
	var summaries []skillcatalog.Summary
	if err == nil {
		summaries, err = catalog.List()
	}
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
func (b *ContextBuilder) BuildSystemPrompt(ctx context.Context, req *agentrundomain.RunRequest) (string, error) {
	sections := make([]string, 0, 11)
	sectionNames := make([]string, 0, 11)

	identity := strings.TrimSpace(buildAssistantPersonaContext(req))
	if identity == "" {
		identity = strings.TrimSpace(prompts.Get(prompts.KeyAgentSystemDefault))
	}
	if identity != "" {
		sections = append(sections, identity)
		sectionNames = append(sectionNames, "identity")
	}

	if communication := strings.TrimSpace(prompts.Get(prompts.KeyAgentSystemCommunication)); communication != "" {
		sections = append(sections, communication)
		sectionNames = append(sectionNames, "communication")
	}

	if multiSpeaker := strings.TrimSpace(prompts.Get(prompts.KeyAgentSystemMultiSpeakerContext)); multiSpeaker != "" {
		sections = append(sections, multiSpeaker)
		sectionNames = append(sectionNames, "conversation_context")
	}

	if taskCompletion := strings.TrimSpace(prompts.Get(prompts.KeyAgentNativeTaskCompletion)); taskCompletion != "" {
		sections = append(sections, taskCompletion)
		sectionNames = append(sectionNames, "task_completion")
	}

	if toolEnforce := strings.TrimSpace(prompts.Get(prompts.KeyAgentNativeToolEnforcement)); toolEnforce != "" {
		sections = append(sections, toolEnforce)
		sectionNames = append(sectionNames, "tool_enforcement")
	}

	if skillLoading := strings.TrimSpace(b.buildSkillLoadingContext(ctx, req)); skillLoading != "" {
		sections = append(sections, skillLoading)
		sectionNames = append(sectionNames, "skill_loading")
	}

	if memGuidance := strings.TrimSpace(prompts.Get(prompts.KeyAgentSystemMemoryGuidance)); memGuidance != "" {
		sections = append(sections, memGuidance)
		sectionNames = append(sectionNames, "memory_guidance")
	}

	if project := strings.TrimSpace(buildProjectContextSection(req)); project != "" {
		sections = append(sections, project)
		sectionNames = append(sectionNames, "project")
	}

	if workspace := strings.TrimSpace(buildWorkspaceContext(req)); workspace != "" {
		sections = append(sections, workspace)
		sectionNames = append(sectionNames, "workspace")
	}

	if artifactDecl := strings.TrimSpace(prompts.Get(prompts.KeyAgentNativeArtifactDeclaration)); artifactDecl != "" {
		sections = append(sections, artifactDecl)
		sectionNames = append(sectionNames, "artifact_declaration")
	}

	if memory := strings.TrimSpace(buildMemoryContext(ctx, b.Memory)); memory != "" {
		sections = append(sections, memory)
		sectionNames = append(sectionNames, "memory")
	}

	if runMeta := strings.TrimSpace(buildRunMetaContext(req)); runMeta != "" {
		sections = append(sections, runMeta)
		sectionNames = append(sectionNames, "run_meta")
	}

	if platform := strings.TrimSpace(buildPlatformContext(req)); platform != "" {
		sections = append(sections, platform)
		sectionNames = append(sectionNames, "platform")
	}

	prompt := strings.Join(sections, "\n\n")
	logs.InfoContextf(ctx, "Agent system prompt built: run_id=%s trace_id=%s sections=%s section_count=%d prompt_len=%d",
		requestRunID(req), requestTraceID(req), strings.Join(sectionNames, ","), len(sections), len(prompt))
	return prompt, nil
}

func catalogForRequest(req *agentrundomain.RunRequest) (*skillcatalog.Catalog, error) {
	if req != nil && strings.TrimSpace(req.Workspace.SkillDir) != "" {
		return skillcatalog.NewCatalog(req.Workspace.SkillDir)
	}
	return skillcatalog.NewCatalogFromDefault()
}

func buildAssistantPersonaContext(req *agentrundomain.RunRequest) string {
	if req == nil {
		return ""
	}
	name := strings.TrimSpace(req.Assistant.Name)
	description := strings.TrimSpace(req.Assistant.Description)
	systemPrompt := strings.TrimSpace(req.Assistant.SystemPrompt)
	if name == "" && description == "" && systemPrompt == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<identity_override>\n")
	sb.WriteString("你运行在 lework 平台中，但当前对用户展示和执行任务的第一身份是被召唤的 AI 队友。\n")
	sb.WriteString("当用户询问“你是谁”“你是干什么的”“你能做什么”时，介绍当前 AI 队友的名称、能力范围、擅长领域和可提供的帮助。\n")
	if name != "" {
		sb.WriteString("\n\n队友名称：")
		sb.WriteString(name)
	}
	if description != "" {
		sb.WriteString("\n\n队友描述：")
		sb.WriteString(description)
	}
	if systemPrompt != "" {
		sb.WriteString("\n\n队友能力：\n")
		sb.WriteString(systemPrompt)
	}
	sb.WriteString("\n</identity_override>")
	return sb.String()
}

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

func buildProjectContextSection(req *agentrundomain.RunRequest) string {
	if req == nil || len(req.Project.Members) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## 协作成员\n")
	for _, m := range req.Project.Members {
		label := "用户"
		if m.MemberType == "assistant" {
			label = "AI 队友"
		}
		sb.WriteString(fmt.Sprintf("- %s：%s（%s）\n", label, m.Name, m.MemberRole))
	}
	return strings.TrimSpace(sb.String())
}

func buildWorkspaceContext(req *agentrundomain.RunRequest) string {
	if req == nil {
		return ""
	}
	projectID := strings.TrimSpace(req.Workspace.ProjectID)
	taskID := strings.TrimSpace(req.Workspace.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(req.TaskID)
	}
	requestID := strings.TrimSpace(req.Workspace.RequestID)
	if req.Workspace.OrgID == 0 || projectID == "" || taskID == "" || requestID == "" {
		return ""
	}
	plan, err := agentworkspace.ResolveTaskWorkspace(agentworkspace.TaskWorkspaceRequest{
		OrgID:            req.Workspace.OrgID,
		ProjectID:        projectID,
		TaskID:           taskID,
		RequestID:        requestID,
		RequestedWorkDir: req.Runtime.WorkDir,
	})
	if err != nil {
		return ""
	}
	gitRepoLabel := "否"
	if isGitRepository(plan.RepoDir) {
		gitRepoLabel = "是"
	}
	return fmt.Sprintf(`## 工作区信息

- 项目根目录: %s
- 是否为 Git 仓库: %s

**工作区可见性规则：**
- 仅项目工作目录下的内容对用户可见，可被用户访问和下载。
- 不需要让用户看见的临时文件、中间产物，应在项目临时目录: %s 中创建。

**运行系统环境：**
- Host: %s`, plan.RepoDir, gitRepoLabel, plan.TurnTmpDir, runtime.GOOS)
}

func isGitRepository(repoDir string) bool {
	if strings.TrimSpace(repoDir) == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(repoDir, ".git"))
	return err == nil
}

func buildRunMetaContext(req *agentrundomain.RunRequest) string {
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
		parts = append(parts, fmt.Sprintf("- 模型: %s", req.Model.Model))
	}
	if req.Model.Provider != "" {
		parts = append(parts, fmt.Sprintf("- Provider: %s", req.Model.Provider))
	}
	return "## 运行信息\n" + strings.Join(parts, "\n")
}

var platformPromptKeys = map[string]string{
	"wechat": prompts.KeyAgentSystemPlatformWechat,
	"feishu": prompts.KeyAgentSystemPlatformFeishu,
	"slack":  prompts.KeyAgentSystemPlatformSlack,
	"api":    prompts.KeyAgentSystemPlatformAPI,
}

func buildPlatformContext(req *agentrundomain.RunRequest) string {
	if req == nil || req.Actor.Channel == "" {
		return ""
	}
	key, ok := platformPromptKeys[req.Actor.Channel]
	if !ok {
		return ""
	}
	return strings.TrimSpace(prompts.Get(key))
}

func requestRunID(req *agentrundomain.RunRequest) string {
	if req == nil {
		return ""
	}
	return req.RunID
}

func requestTraceID(req *agentrundomain.RunRequest) string {
	if req == nil {
		return ""
	}
	return req.TraceID
}
