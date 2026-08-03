package skillmanage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/tools"
)

func TestToolExecuteCreate(t *testing.T) {
	t.Setenv("LEROS_WORKSPACE_ROOT", t.TempDir())
	tool, err := NewTool()
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	output, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action":  "create",
		"name":    "review-flow",
		"content": "---\nname: review-flow\ndescription: Review flow\n---\n# Review flow\n\n1. Inspect changes.\n",
	}))

	if err != nil {
		t.Fatalf("execute create: %v", err)
	}

	var result skillstore.Result
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result.Success || result.Action != "create" || result.Name != "review-flow" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestToolValidateRequiresNewTextForPatch(t *testing.T) {
	tool, toolErr := NewTool()
	if toolErr != nil {
		t.Fatalf("NewTool: %v", toolErr)
	}
	err := tool.Validate(tools.JSONInput(map[string]interface{}{
		"action":   "patch",
		"name":     "review-flow",
		"old_text": "old",
	}))

	if err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestToolWritesSupportingFileAtArbitraryRelativePath(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv("LEROS_WORKSPACE_ROOT", workspaceRoot)
	tool, err := NewTool()
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	ctx := context.Background()
	if _, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"action":  "create",
		"name":    "free-layout",
		"content": "---\nname: free-layout\ndescription: Free layout\n---\n# Free layout\n\nSteps.\n",
	})); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := tool.Execute(ctx, tools.JSONInput(map[string]interface{}{
		"action":       "write_file",
		"name":         "free-layout",
		"file_path":    "custom/deep/info.txt",
		"file_content": "content",
	})); err != nil {
		t.Fatalf("write supporting file: %v", err)
	}
	path := filepath.Join(workspaceRoot, ".leros", "skills", "free-layout", "custom", "deep", "info.txt")
	if content, err := os.ReadFile(path); err != nil || string(content) != "content" {
		t.Fatalf("supporting file content=%q err=%v", string(content), err)
	}
	if description := buildSkillManageDescription(); !strings.Contains(description, "任意层级") ||
		strings.Contains(description, "references/、templates/") {
		t.Fatalf("unexpected tool description: %s", description)
	}
}
