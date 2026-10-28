package opencode

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/agent"
)

// PlanAgentName 是 v2 计划模式的 primary agent 名。
const PlanAgentName = "leros-plan"

// PlanSystemPrompt 要求模型写完计划文件后调用 question 工具，
// header/问题/选项必须与前端既有契约一致（设计文档 §8.3）。
const PlanSystemPrompt = `You are the Leros plan agent.
Write the implementation plan as Markdown to a file under .opencode/plans/ (for example .opencode/plans/plan.md).
Do not modify files outside .opencode/plans.
After the plan file is written, call the question tool with:
- title: 计划确认
- description: 以下是当前计划，是否执行？
- options: exactly "Yes" and "No"
- multiple: false
Do not call plan_exit; it does not exist in this runtime.`

// configOption 在基础配置上追加 v2 段（目前只有计划模式使用）。
type configOption func(*configContent)

// withPlanMode 注入 leros-plan：deny 在前、allow 在后的相对 glob，
// 并显式 deny shell（否则 edit deny 可被 shell 绕过，设计文档 §8.2）。
func withPlanMode() configOption {
	return func(cfg *configContent) {
		cfg.DefaultAgent = PlanAgentName
		cfg.Agents = map[string]planAgentConfig{
			PlanAgentName: {
				Mode:        "primary",
				Description: "Leros plan mode",
				System:      PlanSystemPrompt,
				Permissions: []permissionRule{
					{Action: "question", Resource: "*", Effect: "allow"},
					{Action: "read", Resource: "*", Effect: "allow"},
					{Action: "edit", Resource: "*", Effect: "deny"},
					{Action: "shell", Resource: "*", Effect: "deny"},
					{Action: "external_directory", Resource: "*", Effect: "deny"},
					{Action: "edit", Resource: ".opencode/plans/*", Effect: "allow"},
				},
			},
		}
	}
}

// IsPlanConfirmation 判定一次 question 表单是否是计划确认：
// mode == plan、本轮有成功写入的计划文件、表单恰 1 题且选项恰为 Yes/No。
func IsPlanConfirmation(mode agent.ExecutionMode, planPath string, questions []agent.QuestionItem) bool {
	if mode != agent.ExecutionModePlan || strings.TrimSpace(planPath) == "" {
		return false
	}
	if len(questions) != 1 {
		return false
	}
	options := questions[0].Options
	if len(options) != 2 {
		return false
	}
	yes, no := strings.TrimSpace(options[0].Label), strings.TrimSpace(options[1].Label)
	return yes == "Yes" && no == "No"
}

// ValidatePlanArtifact 是放宽后的计划产物校验：父目录必须是 plans、扩展名 .md。
func ValidatePlanArtifact(path, workDir string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("plan path is empty")
	}
	if !filepath.IsAbs(path) {
		if strings.TrimSpace(workDir) == "" {
			return errors.New("plan path is relative and workDir is unavailable")
		}
		path = filepath.Join(workDir, path)
	}
	path = filepath.Clean(path)
	if filepath.Ext(path) != ".md" || filepath.Base(filepath.Dir(path)) != "plans" {
		return errors.New("plan file must be a .md under a plans directory")
	}
	if strings.TrimSpace(workDir) != "" {
		relative, err := filepath.Rel(workDir, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("plan file is outside the workspace")
		}
	}
	return nil
}

// PlanConfirmationInteractionType 是前端识别计划确认的固定值，必须逐字保留。
const PlanConfirmationInteractionType = "plan_confirmation"

// PlanReadyPayloadFor 构造 plan.ready 的 payload：路径必须落在
// <workDir>/.opencode/plans/*.md，DisplayPath 用相对工作区的展示路径。
func PlanReadyPayloadFor(workDir, planPath, providerSessionID string) (agent.PlanReadyPayload, error) {
	if err := ValidatePlanArtifact(planPath, workDir); err != nil {
		return agent.PlanReadyPayload{}, err
	}
	display, err := filepath.Rel(workDir, planPath)
	if err != nil {
		return agent.PlanReadyPayload{}, fmt.Errorf("resolve plan display path: %w", err)
	}
	return agent.PlanReadyPayload{
		Path:              filepath.Clean(planPath),
		DisplayPath:       display,
		ProviderSessionID: providerSessionID,
	}, nil
}

// PlanConfirmationBundle 在判定成立时返回「先 plan.ready、再把问句标记为
// plan_confirmation」的事件序列与改写后的 payload；不成立时 ok=false，
// 由调用方计数 plan_contract_miss 并继续（不失败，设计文档 §8.3）。
func PlanConfirmationBundle(
	workDir string,
	planPath string,
	providerSessionID string,
	mode agent.ExecutionMode,
	payload agent.QuestionAskedPayload,
) ([]agent.NodeEvent, agent.QuestionAskedPayload, bool) {
	if !IsPlanConfirmation(mode, planPath, payload.Questions) {
		return nil, payload, false
	}
	ready, err := PlanReadyPayloadFor(workDir, planPath, providerSessionID)
	if err != nil {
		return nil, payload, false
	}
	payload.InteractionType = PlanConfirmationInteractionType
	return []agent.NodeEvent{agent.NewPlanReadyEvent(ready.Path, ready.DisplayPath, ready.ProviderSessionID)}, payload, true
}
