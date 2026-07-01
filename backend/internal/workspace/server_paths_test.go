package workspace

import (
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
