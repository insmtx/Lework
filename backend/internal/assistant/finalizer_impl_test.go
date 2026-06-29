package assistant

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

func TestFinalizerUsesPreparedWorkspaceAndCollectsArtifacts(t *testing.T) {
	repoDir := t.TempDir()
	turnDir := filepath.Join(t.TempDir(), "turn")
	if err := os.MkdirAll(turnDir, 0o755); err != nil {
		t.Fatalf("create turn dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "report.md"), []byte("report"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	manifestPath := filepath.Join(turnDir, "artifacts.jsonl")
	if err := os.WriteFile(
		manifestPath,
		[]byte(`{"path":"report.md","title":"Report","is_final":true}`+"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	run := &PreparedRun{
		Request: &assistantdomain.RunRequest{
			RunID:   "run-1",
			TraceID: "trace-1",
			Workspace: assistantdomain.WorkspaceContext{
				OrgID:     99,
				ProjectID: "request-must-not-be-resolved",
			},
		},
		Execution: agent.ExecutionRequest{Runtime: "codex"},
		Workspace: WorkspacePreparation{
			WorkDir:              repoDir,
			RepoDir:              repoDir,
			TaskDir:              filepath.Dir(turnDir),
			ArtifactManifestPath: manifestPath,
			BaselinePath:         filepath.Join(turnDir, "baseline.jsonl"),
		},
	}

	finalization, err := NewFinalizer().FinalizeRequired(
		context.Background(),
		run,
		&agent.ExecutionResult{Message: "done", ProviderConversationID: "provider-1"},
		JournalSnapshot{},
	)
	if err != nil {
		t.Fatalf("FinalizeRequired() error = %v", err)
	}
	if finalization.Result == nil ||
		len(finalization.Result.Artifacts) != 1 ||
		finalization.Result.Artifacts[0].StorageKey != "report.md" ||
		len(finalization.Events) != 1 ||
		finalization.Events[0].Type != "artifact.declared" {
		t.Fatalf("finalization = %#v", finalization)
	}
	if finalization.Result.Metadata == nil ||
		finalization.Result.Metadata.WorkDir != repoDir ||
		finalization.Result.Metadata.ProviderID != "provider-1" {
		t.Fatalf("metadata = %#v", finalization.Result.Metadata)
	}
}

func TestFinalizerReturnsPreparedWorkspaceGitFailure(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatalf("create invalid git dir: %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "artifacts.jsonl")
	if err := os.WriteFile(manifestPath, nil, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	run := &PreparedRun{
		Request: &assistantdomain.RunRequest{RunID: "run-1"},
		Workspace: WorkspacePreparation{
			WorkDir:              repoDir,
			RepoDir:              repoDir,
			ArtifactManifestPath: manifestPath,
		},
	}

	_, err := NewFinalizer().FinalizeRequired(
		context.Background(),
		run,
		&agent.ExecutionResult{Message: "done"},
		JournalSnapshot{},
	)
	if err == nil || !strings.Contains(err.Error(), "push workspace") {
		t.Fatalf("FinalizeRequired() error = %v, want git failure", err)
	}
}
