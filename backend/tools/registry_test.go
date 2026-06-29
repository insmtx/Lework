package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type registryTestTool struct {
	BaseTool
	output string
}

func newRegistryTestTool(name string, output string) *registryTestTool {
	return &registryTestTool{
		BaseTool: NewBaseTool(name, "test tool", Schema{Type: "object"}),
		output:   output,
	}
}

func (t *registryTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.output, nil
}

func TestRegistryRegisterOverwritesExistingTool(t *testing.T) {
	registry := NewRegistry()

	first := newRegistryTestTool("example", "first")
	if err := registry.Register(first); err != nil {
		t.Fatalf("register first tool: %v", err)
	}

	second := newRegistryTestTool("example", "second")
	if err := registry.Register(second); err != nil {
		t.Fatalf("register second tool: %v", err)
	}

	tool, err := registry.Get("example")
	if err != nil {
		t.Fatalf("get overwritten tool: %v", err)
	}
	if tool != second {
		t.Fatalf("expected second tool to overwrite first")
	}
}
