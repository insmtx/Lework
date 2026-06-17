package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestWorkerMountedWorkspacePath(t *testing.T) {
	serverRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, serverRoot)

	got, err := WorkerMountedWorkspacePath(1, 1)
	if err != nil {
		t.Fatalf("WorkerMountedWorkspacePath failed: %v", err)
	}
	want := filepath.Join(serverRoot, "1", "1", "workspace")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	if _, err := WorkerMountedWorkspacePath(0, 1); err == nil {
		t.Fatal("expected empty org_id to be rejected")
	}
	if _, err := WorkerMountedWorkspacePath(1, 0); err == nil {
		t.Fatal("expected empty worker_id to be rejected")
	}
}

func TestArtifactStoragePathResolvesUnderWorkerWorkspace(t *testing.T) {
	serverRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, serverRoot)

	storageKey := filepath.Join("projects", "1", "project_1", "repo", "result.txt")
	absolutePath := filepath.Join(serverRoot, "1", "1", "workspace", storageKey)
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatalf("failed to create artifact directory: %v", err)
	}
	if err := os.WriteFile(absolutePath, []byte("artifact body"), 0o644); err != nil {
		t.Fatalf("failed to write artifact file: %v", err)
	}

	resolved, err := ArtifactStoragePath(1, 1, filepath.ToSlash(storageKey))
	if err != nil {
		t.Fatalf("ArtifactStoragePath failed: %v", err)
	}
	if resolved != absolutePath {
		t.Fatalf("expected %q, got %q", absolutePath, resolved)
	}
}

func TestArtifactStoragePathRejectsInvalidKey(t *testing.T) {
	t.Setenv(leros.EnvWorkspaceRoot, t.TempDir())

	if _, err := ArtifactStoragePath(1, 1, ""); err == nil {
		t.Fatal("expected empty storage key to be rejected")
	}
	if _, err := ArtifactStoragePath(1, 1, "../outside.txt"); err == nil {
		t.Fatal("expected escaping storage key to be rejected")
	}
	if _, err := ArtifactStoragePath(1, 1, filepath.Join(t.TempDir(), "absolute.txt")); err == nil {
		t.Fatal("expected absolute storage key to be rejected")
	}
}
