package engines

import (
	"context"
	"testing"
)

type stubEngine struct{}

func (stubEngine) Prepare(context.Context, PrepareRequest) error {
	return nil
}

func (stubEngine) RegisterMCP(context.Context, MCPServerConfig) error {
	return nil
}

func (stubEngine) GetSkillDir() string {
	return ""
}

func (stubEngine) Run(context.Context, RunRequest) (*RunHandle, error) {
	return &RunHandle{}, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("stub", stubEngine{}); err != nil {
		t.Fatalf("register engine: %v", err)
	}

	engine, ok := registry.Get("stub")
	if !ok {
		t.Fatal("expected registered engine")
	}
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
}
