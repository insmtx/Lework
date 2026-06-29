// Package memory exposes Leros built-in memory as a runtime tool.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	"github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/tools"
)

const (
	// ToolNameMemory is the stable runtime tool name for built-in memory.
	ToolNameMemory = "memory"
)

// Tool lets the agent manage built-in USER.md and MEMORY.md files.
type Tool struct {
	tools.BaseTool
	store *localmemory.Store
}

// NewTool creates the built-in memory tool with the default local store.
func NewTool() *Tool {
	store, _ := localmemory.NewStore(localmemory.Options{})
	return NewToolWithStore(store)
}

// NewToolWithStore creates the built-in memory tool with an explicit store.
func NewToolWithStore(store *localmemory.Store) *Tool {
	return &Tool{
		BaseTool: tools.NewBaseTool(
			ToolNameMemory,
			"保存持久信息到内置长期记忆中，使其能够跨会话保留。记忆会被注入到未来的对话中，"+
				"因此内容应保持简洁，并聚焦于以后仍然重要的事实。\n\n"+
				"当运行环境里存在多个记忆能力时，应优先使用此 memory 工具新增、替换或删除长期记忆。\n\n"+
				"何时保存（应主动执行，不要等用户明确要求）：\n"+
				"- 用户纠正你，或说“记住这个”/“以后别这样”\n"+
				"- 用户分享了偏好、习惯或个人信息，比如姓名、角色、时区、编码风格\n"+
				"- 你发现了关于环境的信息，比如操作系统、已安装工具、项目结构\n"+
				"- 你学到了某个与该用户环境相关的约定、API 特性或工作流\n"+
				"- 你识别出一个稳定事实，并且它在未来会话中仍然有用\n\n"+
				"优先级：用户偏好和纠正 > 环境事实 > 流程性知识。最有价值的记忆，是能避免用户以后重复说明的信息。\n\n"+
				"不要保存任务进度、会话结果、已完成工作日志、临时 TODO 状态、琐碎或显而易见的信息、"+
				"很容易重新发现的信息、原始数据堆砌或大段日志。\n\n"+
				"两个目标：\n"+
				"- user：关于用户是谁，包括姓名、角色、偏好、沟通风格、讨厌的做法\n"+
				"- memory：你的笔记，包括环境事实、项目约定、工具坑点、经验教训\n\n"+
				"操作：add 新增条目；replace 更新已有条目，使用 old_text 来定位旧内容；"+
				"remove 删除已有条目，使用 old_text 来定位旧内容。",
			tools.Schema{
				Type:     "object",
				Required: []string{"action", "target"},
				Properties: map[string]*tools.Property{
					"action": {
						Type:        "string",
						Description: "操作类型：add 新增记忆，replace 替换已有记忆，remove 删除已有记忆。",
						Enum:        []string{"add", "replace", "remove"},
					},
					"target": {
						Type:        "string",
						Description: "记忆目标：user 表示用户画像；memory 表示 worker/assistant 的长期事实和经验；project 表示项目级记忆。",
						Enum:        []string{"user", "memory", "project"},
					},
					"content": {
						Type:        "string",
						Description: "add/replace 使用的新记忆内容。应简洁、稳定、有长期价值。",
					},
					"old_text": {
						Type:        "string",
						Description: "replace/remove 用于定位已有条目的唯一短文本片段。",
					},
				},
			},
		),
		store: store,
	}
}

// Validate checks memory tool input before execution.
func (t *Tool) Validate(raw json.RawMessage) error {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return err
	}
	return validateInput(input)
}

func validateInput(input map[string]any) error {
	action := strings.TrimSpace(stringValue(input, "action"))
	target := strings.TrimSpace(stringValue(input, "target"))
	if action == "" {
		return fmt.Errorf("action is required")
	}
	if target == "" {
		return fmt.Errorf("target is required")
	}
	if target != localmemory.TargetUser && target != localmemory.TargetMemory && target != "project" {
		return fmt.Errorf("invalid target %q: use user, memory, or project", target)
	}

	switch action {
	case "add":
		if strings.TrimSpace(stringValue(input, "content")) == "" {
			return fmt.Errorf("content is required for add")
		}
	case "replace":
		if strings.TrimSpace(stringValue(input, "old_text")) == "" {
			return fmt.Errorf("old_text is required for replace")
		}
		if strings.TrimSpace(stringValue(input, "content")) == "" {
			return fmt.Errorf("content is required for replace")
		}
	case "remove":
		if strings.TrimSpace(stringValue(input, "old_text")) == "" {
			return fmt.Errorf("old_text is required for remove")
		}
	default:
		return fmt.Errorf("unknown action %q: use add, replace, or remove", action)
	}

	return nil
}

// Execute performs the memory operation.
func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t == nil || t.store == nil {
		return "", fmt.Errorf("memory store is not initialized")
	}
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return "", err
	}
	if err := validateInput(input); err != nil {
		return "", err
	}

	action := strings.TrimSpace(stringValue(input, "action"))
	target := strings.TrimSpace(stringValue(input, "target"))
	content := stringValue(input, "content")
	oldText := stringValue(input, "old_text")

	// 当 target == "project" 时，写入项目 repo 下的 .leros/memory/project_memory.md
	store := t.store
	storeTarget := target
	if target == "project" {
		toolCtx, ok := tools.ToolContextFrom(ctx)
		if !ok || toolCtx.WorkDir == "" {
			return "", fmt.Errorf("project memory requires a work directory in tool context")
		}
		repoDir, err := workspace.FindRepoRoot(toolCtx.WorkDir)
		if err != nil {
			return "", fmt.Errorf("find project repo root: %w", err)
		}
		memoryDir := filepath.Join(repoDir, ".leros", "memory")
		projStore, err := localmemory.NewStore(localmemory.Options{
			RootDir: memoryDir,
		})
		if err != nil {
			return "", fmt.Errorf("create project memory store: %w", err)
		}
		store = projStore
		storeTarget = localmemory.TargetProjectMemory
	}

	var result *localmemory.Result
	var operationErr error
	switch action {
	case "add":
		result, operationErr = store.Add(ctx, storeTarget, content)
	case "replace":
		result, operationErr = store.Replace(ctx, storeTarget, oldText, content)
	case "remove":
		result, operationErr = store.Remove(ctx, storeTarget, oldText)
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
	if operationErr != nil {
		return "", operationErr
	}
	return tools.JSONString(result)
}

func stringValue(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
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
