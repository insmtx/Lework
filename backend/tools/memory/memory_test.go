package memory

import (
	"context"
	"encoding/json"
	"testing"

	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	"github.com/insmtx/Leros/backend/tools"
)

func TestToolExecuteAdd(t *testing.T) {
	store, err := localmemory.NewStore(localmemory.Options{RootDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	tool := NewToolWithStore(store)

	raw, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action":  "add",
		"target":  "user",
		"content": "用户偏好简洁直接的回答",
	}))

	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var result localmemory.Result
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result.Success || result.Target != localmemory.TargetUser || result.EntryCount != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestToolValidateRequiresOldTextForRemove(t *testing.T) {
	store, err := localmemory.NewStore(localmemory.Options{})
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	tool := NewToolWithStore(store)
	err = tool.Validate(tools.JSONInput(map[string]interface{}{
		"action": "remove",
		"target": "user",
	}))

	if err == nil {
		t.Fatal("expected validation error")
	}
}
