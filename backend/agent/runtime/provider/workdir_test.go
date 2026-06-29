package provider

import (
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestResolveRunWorkDirKeepsExplicitWorkDir(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "repo")
	resolved, err := ResolveRunWorkDir(workDir)
	if err != nil {
		t.Fatalf("resolve run work dir: %v", err)
	}
	if resolved != workDir {
		t.Fatalf("work dir = %q, want %q", resolved, workDir)
	}
}

func TestResolveRunWorkDirFallsBackToWorkspaceTemp(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	resolved, err := ResolveRunWorkDir("")
	if err != nil {
		t.Fatalf("resolve run work dir: %v", err)
	}
	expected := filepath.Join(workspaceRoot, "temp")
	if resolved != expected {
		t.Fatalf("work dir = %q, want %q", resolved, expected)
	}
}
