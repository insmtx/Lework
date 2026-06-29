package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestPrepareTaskWorkspaceClonesPullsAndPreservesTurnDirectories(t *testing.T) {
	source := t.TempDir()
	runGit(t, source, "init", "-b", "main")
	runGit(t, source, "config", "user.name", "Leros Test")
	runGit(t, source, "config", "user.email", "test@example.com")
	writeWorkspaceFile(t, filepath.Join(source, "README.md"), "first")
	runGit(t, source, "add", "README.md")
	runGit(t, source, "commit", "-m", "initial")

	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	request := TaskWorkspaceRequest{
		OrgID:            7,
		ProjectID:        "project-1",
		TaskID:           "task-1",
		RequestID:        "request-1",
		RequestedWorkDir: "src",
		CloneURL:         source,
	}
	plan, err := PrepareTaskWorkspace(context.Background(), request)
	if err != nil {
		t.Fatalf("PrepareTaskWorkspace() error = %v", err)
	}
	for _, path := range []string{
		plan.RepoDir,
		plan.TaskDir,
		plan.TurnDir,
		plan.TurnTmpDir,
		plan.TurnLogDir,
		plan.ArtifactManifestPath,
		plan.EffectiveWorkDir,
	} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("prepared path %s: %v", path, statErr)
		}
	}
	if got, readErr := os.ReadFile(filepath.Join(plan.RepoDir, "README.md")); readErr != nil || string(got) != "first" {
		t.Fatalf("cloned README = %q, error = %v", got, readErr)
	}

	writeWorkspaceFile(t, filepath.Join(source, "README.md"), "second")
	runGit(t, source, "add", "README.md")
	runGit(t, source, "commit", "-m", "update")
	plan, err = PrepareTaskWorkspace(context.Background(), request)
	if err != nil {
		t.Fatalf("second PrepareTaskWorkspace() error = %v", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(plan.RepoDir, "README.md")); readErr != nil || string(got) != "second" {
		t.Fatalf("pulled README = %q, error = %v", got, readErr)
	}
	if _, statErr := os.Stat(plan.ArtifactManifestPath); statErr != nil {
		t.Fatalf("artifact manifest after pull: %v", statErr)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeWorkspaceFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
