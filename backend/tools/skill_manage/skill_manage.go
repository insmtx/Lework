// Package skillmanage exposes SkillStore mutations as a runtime tool.
package skillmanage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/tools"
)

const (
	ToolNameSkillManage = "skill_manage"

	actionCreate     = skillstore.ActionCreate
	actionPatch      = skillstore.ActionPatch
	actionEdit       = skillstore.ActionEdit
	actionDelete     = skillstore.ActionDelete
	actionWriteFile  = skillstore.ActionWriteFile
	actionRemoveFile = skillstore.ActionRemoveFile
)

// buildSkillManageDescription 构建包含实际技能目录路径的工具描述。
func buildSkillManageDescription() string {
	skillsDir := "~/.leros/skills"
	if dir, err := leros.SkillsDir(); err == nil {
		skillsDir = dir
	}
	return fmt.Sprintf(`管理技能（创建、更新、删除）。技能是你的流程性记忆，用于保存 recurring task 的可复用做法。
	新技能创建在 %s/ 目录下；已有技能在其所在位置直接修改。

操作：
  create      创建新技能，写入完整 SKILL.md 内容（含 YAML frontmatter 和 Markdown 正文）
  patch       局部替换文本（old_text → new_text），优先用于修复和微调
  edit        完整重写 SKILL.md 全文，仅用于大幅修改
  delete      删除整个技能目录
  write_file  写入 supporting file（references/、templates/、scripts/ 或 assets/ 下）
  remove_file 删除 supporting file

何时创建：
  - 复杂任务成功完成、克服了错误
  - 用户纠正后的做法被验证有效
  - 发现非平凡工作流
  - 用户要求你记住某个流程
何时更新：
  - 说明过时或错误
  - 出现操作系统相关失败
  - 使用过程中发现缺失步骤或坑点
  如果使用了某个技能并遇到它没有覆盖的问题，应立即修补它。

困难或反复迭代的任务完成后，可以提议保存为技能。简单一次性任务不要保存。创建或删除技能前应先向用户确认。

好的技能应包含：触发条件、带精确命令的编号步骤、坑点说明、验证步骤。可使用 skill工具查看已有技能的格式示例。`, skillsDir)
}

// Tool lets the agent create and update procedural skills.
type Tool struct {
	tools.BaseTool
	store *skillstore.SkillStore
}

// NewTool creates skill_manage with the default Leros skills store.
func NewTool() (*Tool, error) {
	return NewToolWithMutation(nil)
}

// NewToolWithMutation creates skill_manage with an instance-scoped mutation callback.
func NewToolWithMutation(onMutation func(context.Context, skillstore.MutationKind, string, string)) (*Tool, error) {
	store, err := skillstore.NewSkillStoreWithMutation("", onMutation)
	if err != nil {
		return nil, fmt.Errorf("skill manage: create store: %w", err)
	}
	return &Tool{
		BaseTool: tools.NewBaseTool(
			ToolNameSkillManage,
			buildSkillManageDescription(),
			tools.Schema{
				Type:     "object",
				Required: []string{"action", "name"},
				Properties: map[string]*tools.Property{
					"action": {
						Type:        "string",
						Enum:        []string{actionCreate, actionPatch, actionEdit, actionDelete, actionWriteFile, actionRemoveFile},
						Description: "要执行的操作。",
					},
					"name": {
						Type:        "string",
						Description: "Skill 名称。使用小写字母、数字、连字符、下划线或点，最长 64 字符。create 时为新技能名，其他操作时必须匹配已有 Skill。",
					},
					"content": {
						Type:        "string",
						Description: "完整的 SKILL.md 内容，包括 YAML frontmatter 和 Markdown 正文。create 和 edit 时必填。",
					},
					"old_text": {
						Type:        "string",
						Description: "patch 要查找的文本。除非 replace_all=true，否则必须唯一。应包含足够上下文来确保唯一匹配。",
					},
					"new_text": {
						Type:        "string",
						Description: "patch 替换文本。可以为空字符串，用于删除匹配文本。",
					},
					"replace_all": {
						Type:        "boolean",
						Description: "patch 使用。是否替换所有匹配项，而不是要求唯一匹配。默认 false。",
					},
					"file_path": {
						Type:        "string",
						Description: "Skill 目录内 supporting file 的路径。write_file/remove_file 时必填，且必须位于 references/、templates/、scripts/ 或 assets/ 下。patch 时可选，省略则默认修改 SKILL.md。",
					},
					"file_content": {
						Type:        "string",
						Description: "文件内容。write_file 时必填。",
					},
				},
			},
		),
		store: store,
	}, nil
}

// Validate checks skill_manage input before execution.
func (t *Tool) Validate(raw json.RawMessage) error {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return err
	}
	return validateInput(input)
}

func validateInput(input map[string]any) error {
	if input == nil {
		return fmt.Errorf("input is required")
	}
	action := stringValue(input, "action")
	name := stringValue(input, "name")
	if action == "" {
		return fmt.Errorf("action is required")
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}

	switch action {
	case actionCreate:
		if strings.TrimSpace(rawStringValue(input, "content")) == "" {
			return fmt.Errorf("content is required for create")
		}
	case actionEdit:
		if strings.TrimSpace(rawStringValue(input, "content")) == "" {
			return fmt.Errorf("content is required for edit")
		}
	case actionPatch:
		if rawStringValue(input, "old_text") == "" {
			return fmt.Errorf("old_text is required for patch")
		}
		if _, ok := input["new_text"]; !ok {
			return fmt.Errorf("new_text is required for patch")
		}
	case actionDelete:
		// absorbed_into 可选：省略/空/非空均合法
	case actionWriteFile:
		if stringValue(input, "file_path") == "" {
			return fmt.Errorf("file_path is required for write_file")
		}
		if _, ok := input["file_content"]; !ok {
			return fmt.Errorf("file_content is required for write_file")
		}
	case actionRemoveFile:
		if stringValue(input, "file_path") == "" {
			return fmt.Errorf("file_path is required for remove_file")
		}
	default:
		return fmt.Errorf("unknown action %q: use create, patch, edit, delete, write_file, or remove_file", action)
	}
	return nil
}

// Execute performs the requested skill management action.
func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t == nil || t.store == nil {
		return "", fmt.Errorf("skill store is not initialized")
	}
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return "", err
	}
	if err := validateInput(input); err != nil {
		return "", err
	}

	action := stringValue(input, "action")
	name := stringValue(input, "name")

	var result *skillstore.Result
	var mutationErr error
	switch action {
	case actionCreate:
		result, mutationErr = t.store.Create(ctx, skillstore.CreateRequest{
			Name:    name,
			Content: rawStringValue(input, "content"),
		})
	case actionEdit:
		result, mutationErr = t.store.Edit(ctx, skillstore.EditRequest{
			Name:    name,
			Content: rawStringValue(input, "content"),
		})
	case actionPatch:
		result, mutationErr = t.store.Patch(ctx, skillstore.PatchRequest{
			Name:       name,
			FilePath:   stringValue(input, "file_path"),
			OldText:    rawStringValue(input, "old_text"),
			NewText:    rawStringValue(input, "new_text"),
			ReplaceAll: boolValue(input, "replace_all"),
		})
	case actionDelete:
		result, mutationErr = t.store.Delete(ctx, skillstore.DeleteRequest{
			Name: name,
		})
	case actionWriteFile:
		result, mutationErr = t.store.WriteFile(ctx, skillstore.WriteFileRequest{
			Name:        name,
			FilePath:    stringValue(input, "file_path"),
			FileContent: rawStringValue(input, "file_content"),
		})
	case actionRemoveFile:
		result, mutationErr = t.store.RemoveFile(ctx, skillstore.RemoveFileRequest{
			Name:     name,
			FilePath: stringValue(input, "file_path"),
		})
	default:
		return "", fmt.Errorf("unsupported action %q", action)
	}
	if mutationErr != nil {
		return "", mutationErr
	}
	return tools.JSONString(result)
}

func stringValue(input map[string]interface{}, key string) string {
	return strings.TrimSpace(rawStringValue(input, key))
}

func rawStringValue(input map[string]interface{}, key string) string {
	value, ok := input[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func boolValue(input map[string]interface{}, key string) bool {
	value, ok := input[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}
