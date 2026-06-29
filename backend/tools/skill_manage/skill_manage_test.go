package skillmanage

import (
	"context"
	"encoding/json"
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
