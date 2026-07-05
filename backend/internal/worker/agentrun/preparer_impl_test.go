package agentrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
	agentruncontext "github.com/insmtx/Leros/backend/internal/worker/agentrun/context"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

type workspaceManagerStub struct {
	preparation WorkspacePreparation
	seen        agentworkspace.TaskWorkspaceRequest
}

func (s *workspaceManagerStub) PrepareWorkspace(
	_ context.Context,
	req agentworkspace.TaskWorkspaceRequest,
) (WorkspacePreparation, error) {
	s.seen = req
	return s.preparation, nil
}

type sessionProviderStub struct {
	workDir string
}

func (s *sessionProviderStub) Prepare(_ context.Context, req *agentrundomain.RunRequest) error {
	s.workDir = req.Runtime.WorkDir
	req.Conversation.Messages = []agentrundomain.InputMessage{{Role: "assistant", Content: "history"}}
	return nil
}

func (*sessionProviderStub) CompleteClaimed(context.Context, *agentrundomain.RunRequest) error {
	return nil
}

type toolProviderStub struct {
	workspace WorkspacePreparation
}

func (s *toolProviderStub) ToolsFor(
	_ *agentrundomain.RunRequest,
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
	builder := agentruncontext.NewContextBuilder(agentruncontext.ContextBuilder{
		SessionMessages: sessionProvider,
	})
	preparer := NewPreparerWithTools(
		builder,
		workspaceManager,
		nil,
		modelrouter.NewModelStore(),
		toolProvider,
	)
	request := &agentrundomain.RunRequest{
		RunID:         "run-1",
		TaskID:        "task-1",
		ExecutionMode: agentrundomain.ExecutionModePlan,
		Assistant: agentrundomain.AssistantContext{
			ID: "assistant-1",
		},
		Workspace: agentrundomain.WorkspaceContext{
			OrgID:     1,
			ProjectID: "project-1",
			TaskID:    "task-1",
			RequestID: "request-1",
		},
		Input: agentrundomain.InputContext{
			Type:     agentrundomain.InputTypeMessage,
			Messages: []agentrundomain.InputMessage{{Role: "user", Content: "/review inspect the change"}},
		},
		Model: agentrundomain.ModelOptions{
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
	if workspaceManager.seen.ProjectID != request.Workspace.ProjectID {
		t.Fatalf("workspace request = %#v", workspaceManager.seen)
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
	if prepared.Execution.Mode != agent.ExecutionModePlan {
		t.Fatalf("execution mode = %q, want %q", prepared.Execution.Mode, agent.ExecutionModePlan)
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

func TestPreparerResolvesProviderSessionForRuntimeResume(t *testing.T) {
	workspace := WorkspacePreparation{WorkDir: "/workspace/repo"}
	sessionStore := &providerSessionRecorder{
		resume: &ProviderSessionBinding{
			InternalSessionID: "conversation-1",
			Provider:          "opencode",
			ProviderSessionID: "provider-session-1",
			Status:            "active",
		},
	}
	builder := agentruncontext.NewContextBuilder(agentruncontext.ContextBuilder{})
	preparer := NewPreparerWithSessionStore(
		builder,
		&workspaceManagerStub{preparation: workspace},
		nil,
		modelrouter.NewModelStore(),
		nil,
		sessionStore,
	)
	request := &agentrundomain.RunRequest{
		RunID: "run-1",
		Runtime: agentrundomain.RuntimeOptions{
			Kind: "opencode",
		},
		Conversation: agentrundomain.ConversationContext{
			ID: "conversation-1",
		},
		Input: agentrundomain.InputContext{
			Type:     agentrundomain.InputTypeMessage,
			Messages: []agentrundomain.InputMessage{{Role: "user", Content: "continue"}},
		},
		Model: agentrundomain.ModelOptions{
			Provider: "openai",
			Model:    "test-model",
			APIKey:   "test-key",
		},
	}

	prepared, err := preparer.Prepare(context.Background(), request)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if sessionStore.getKey.InternalSessionID != "conversation-1" ||
		sessionStore.getKey.Provider != "opencode" {
		t.Fatalf("provider session lookup key = %#v", sessionStore.getKey)
	}
	if prepared.Execution.ProviderSession.ID != "provider-session-1" ||
		!prepared.Execution.ProviderSession.Resume {
		t.Fatalf("execution provider session = %#v", prepared.Execution.ProviderSession)
	}
	if prepared.Execution.Model.APIKey != "test-key" {
		t.Fatalf("execution API key = %q, want test-key", prepared.Execution.Model.APIKey)
	}
	if sessionStore.binding != nil {
		t.Fatalf("preparer should not persist provider session, got %#v", sessionStore.binding)
	}
}
