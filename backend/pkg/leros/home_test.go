package leros

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorkspaceRootUsesConfiguredRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvWorkspaceRoot, root)

	workspace, err := WorkspaceRoot()
	if err != nil {
		t.Fatalf("workspace root: %v", err)
	}

	expected, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	if workspace != expected {
		t.Fatalf("expected %s, got %s", expected, workspace)
	}
}

func TestDefaultWorkspaceRootForUnix(t *testing.T) {
	root, err := defaultWorkspaceRoot("linux")
	if err != nil {
		t.Fatalf("default workspace root: %v", err)
	}
	if root != defaultWorkspaceRootUnix {
		t.Fatalf("expected %s, got %s", defaultWorkspaceRootUnix, root)
	}
}

func TestDefaultWorkspaceRootForWindows(t *testing.T) {
	localAppData := filepath.Join(t.TempDir(), "AppData", "Local")
	t.Setenv("LOCALAPPDATA", localAppData)

	root, err := defaultWorkspaceRoot("windows")
	if err != nil {
		t.Fatalf("default workspace root: %v", err)
	}

	expected := filepath.Join(localAppData, "Leros", "workspace")
	if root != expected {
		t.Fatalf("expected %s, got %s", expected, root)
	}
}

func TestWorkspaceRootDefaultsToPlatformWorkspace(t *testing.T) {
	t.Setenv(EnvWorkspaceRoot, "")
	localAppData := filepath.Join(t.TempDir(), "AppData", "Local")
	t.Setenv("LOCALAPPDATA", localAppData)

	root, err := WorkspaceRoot()
	if err != nil {
		t.Fatalf("workspace root: %v", err)
	}

	defaultRoot, err := defaultWorkspaceRoot(runtime.GOOS)
	if err != nil {
		t.Fatalf("default workspace root: %v", err)
	}
	expected, err := filepath.Abs(defaultRoot)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}
	if root != expected {
		t.Fatalf("expected %s, got %s", expected, root)
	}
}

func TestWorkspaceSubdirs(t *testing.T) {
	root := t.TempDir()
	t.Setenv(EnvWorkspaceRoot, root)

	skillsDir, err := SkillsDir()
	if err != nil {
		t.Fatalf("skills dir: %v", err)
	}
	if skillsDir != filepath.Join(root, ".leros", "skills") {
		t.Fatalf("unexpected skills dir: %s", skillsDir)
	}

	memoryDir, err := MemoryDir()
	if err != nil {
		t.Fatalf("memory dir: %v", err)
	}
	if memoryDir != filepath.Join(root, ".leros", "memory") {
		t.Fatalf("unexpected memory dir: %s", memoryDir)
	}

	tempDir, err := TempDir()
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	if tempDir != filepath.Join(root, "temp") {
		t.Fatalf("unexpected temp dir: %s", tempDir)
	}
}
