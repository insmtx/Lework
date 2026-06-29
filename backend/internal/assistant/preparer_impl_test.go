package assistant

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	lifecyclecontext "github.com/insmtx/Leros/backend/internal/assistant/context"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

type workspaceManagerStub struct {
	preparation WorkspacePreparation
	seen        *assistantdomain.RunRequest
}

func (s *workspaceManagerStub) PrepareWorkspace(
	_ context.Context,
	req *assistantdomain.RunRequest,
) (WorkspacePreparation, error) {
	s.seen = req
	return s.preparation, nil
}

type sessionProviderStub struct {
	workDir string
}

func (s *sessionProviderStub) Prepare(_ context.Context, req *assistantdomain.RunRequest) error {
	s.workDir = req.Runtime.WorkDir
	req.Conversation.Messages = []assistantdomain.InputMessage{{Role: "assistant", Content: "history"}}
	return nil
}

func (*sessionProviderStub) CompleteClaimed(context.Context, *assistantdomain.RunRequest) error {
	return nil
}

type toolProviderStub struct {
	workspace WorkspacePreparation
}

func (s *toolProviderStub) ToolsFor(
	_ *assistantdomain.RunRequest,
	workspace WorkspacePreparation,
) ([]agent.Tool, error) {
	s.workspace = workspace
	return []agent.Tool{preparedTool{}}, nil
}

type preparedTool struct{}

func (preparedTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{Name: "prepared_tool", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (preparedTool) Execute(context.Context, json.RawMessage) (agent.ToolResult, error) {
	return agent.ToolResult{Content: "ok"}, nil
}

func TestPreparerUsesOneWorkspaceSnapshotAndPreservesSkillPrompt(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	skillDir := filepath.Join(workspaceRoot, ".leros", "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(
		"---\nname: review\ndescription: review files\n---\nUse the prepared review workflow.\n",
	), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	workspace := WorkspacePreparation{
		WorkDir:              "/workspace/repo/src",
		RepoDir:              "/workspace/repo",
		TaskDir:              "/workspace/repo/.leros/tasks/task-1",
		ArtifactManifestPath: "/workspace/repo/.leros/tasks/task-1/turns/request-1/artifacts.jsonl",
	}
	workspaceManager := &workspaceManagerStub{preparation: workspace}
	sessionProvider := &sessionProviderStub{}
	toolProvider := &toolProviderStub{}
	builder := lifecyclecontext.NewContextBuilder(lifecyclecontext.ContextBuilder{
		SessionMessages: sessionProvider,
	})
	preparer := NewPreparerWithTools(
		builder,
		workspaceManager,
		nil,
		modelrouter.NewModelStore(),
		toolProvider,
	)
	request := &assistantdomain.RunRequest{
		RunID:  "run-1",
		TaskID: "task-1",
		Assistant: assistantdomain.AssistantContext{
			ID: "assistant-1",
		},
		Workspace: assistantdomain.WorkspaceContext{
			OrgID:     1,
			ProjectID: "project-1",
			TaskID:    "task-1",
			RequestID: "request-1",
		},
		Input: assistantdomain.InputContext{
			Type:     assistantdomain.InputTypeMessage,
			Messages: []assistantdomain.InputMessage{{Role: "user", Content: "/review inspect the change"}},
		},
		Model: assistantdomain.ModelOptions{
			Provider: "openai",
			Model:    "test-model",
			APIKey:   "test-key",
		},
	}

	prepared, err := preparer.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if request.Runtime.WorkDir != "" || request.Input.Messages[0].Content != "/review inspect the change" {
		t.Fatalf("original request mutated: %#v", request)
	}
	if workspaceManager.seen == request {
		t.Fatal("workspace manager received the original mutable request")
	}
	if sessionProvider.workDir != workspace.WorkDir {
		t.Fatalf("session provider work dir = %q", sessionProvider.workDir)
	}
	if prepared.Workspace != workspace || toolProvider.workspace != workspace {
		t.Fatalf("workspace snapshots differ: prepared=%#v tools=%#v", prepared.Workspace, toolProvider.workspace)
	}
	if prepared.Execution.Filesystem.WorkDir != workspace.WorkDir ||
		prepared.Execution.Filesystem.RepoDir != workspace.RepoDir ||
		prepared.Execution.Filesystem.TaskDir != workspace.TaskDir {
		t.Fatalf("execution filesystem = %#v", prepared.Execution.Filesystem)
	}
	if len(prepared.Execution.Tools) != 1 || prepared.Execution.Tools[0].Definition().Name != "prepared_tool" {
		t.Fatalf("execution tools = %#v", prepared.Execution.Tools)
	}
	if len(prepared.Execution.Messages) != 1 || prepared.Execution.Messages[0].Content != "history" {
		t.Fatalf("execution messages = %#v", prepared.Execution.Messages)
	}
	if !strings.Contains(prepared.Execution.Prompt, "Use the prepared review workflow.") ||
		!strings.Contains(prepared.Execution.Prompt, "inspect the change") {
		t.Fatalf("prepared prompt lost skill rewrite: %s", prepared.Execution.Prompt)
	}
}
