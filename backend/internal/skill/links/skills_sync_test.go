package skilllinks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestSyncToLerosDirCreatesSystemSkillsDirectory(t *testing.T) {
	builtinRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	writeSyncTestSkill(t, filepath.Join(builtinRoot, "review-flow"), "review-flow", "test body")

	if err := SyncToLerosDir(builtinRoot); err != nil {
		t.Fatalf("sync to leros dir: %v", err)
	}

	systemSkillDir := filepath.Join(workspaceRoot, ".leros", "skills", ".system", "review-flow")
	targetBody, err := os.ReadFile(filepath.Join(systemSkillDir, skillManifestFile))
	if err != nil {
		t.Fatalf("read synced skill: %v", err)
	}
	if string(targetBody) == "" {
		t.Fatal("expected skill content, got empty")
	}
}

func TestSyncToLerosDirIncludesHiddenSystemSkills(t *testing.T) {
	builtinRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	writeSyncTestSkill(t, filepath.Join(builtinRoot, "review-flow"), "review-flow", "visible")
	writeSyncTestSkill(t, filepath.Join(builtinRoot, ".system", "lework-skill-manager"), "lework-skill-manager", "hidden")

	if err := SyncToLerosDir(builtinRoot); err != nil {
		t.Fatalf("sync to leros dir: %v", err)
	}

	targetRoot := filepath.Join(workspaceRoot, ".leros", "skills", ".system")
	for _, name := range []string{"review-flow", "lework-skill-manager"} {
		if _, err := os.Stat(filepath.Join(targetRoot, name, skillManifestFile)); err != nil {
			t.Fatalf("synced Skill %q: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(targetRoot, ".system", "lework-skill-manager")); !os.IsNotExist(err) {
		t.Fatalf("hidden source directory should not be nested in target, err=%v", err)
	}
}

func TestSyncToLerosDirIncludesHiddenSkillsWithoutVisibleSkills(t *testing.T) {
	builtinRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	writeSyncTestSkill(t, filepath.Join(builtinRoot, ".system", "lework-automation-manager"), "lework-automation-manager", "hidden")

	if err := SyncToLerosDir(builtinRoot); err != nil {
		t.Fatalf("sync hidden Skill only: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, ".leros", "skills", ".system", "lework-automation-manager", skillManifestFile)); err != nil {
		t.Fatalf("synced hidden Skill: %v", err)
	}
}

func TestSyncToLerosDirRejectsDuplicateVisibleAndHiddenSkills(t *testing.T) {
	builtinRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	writeSyncTestSkill(t, filepath.Join(builtinRoot, "duplicate"), "duplicate", "visible")
	writeSyncTestSkill(t, filepath.Join(builtinRoot, ".system", "duplicate"), "duplicate", "hidden")

	if err := SyncToLerosDir(builtinRoot); err == nil {
		t.Fatal("expected duplicate visible and hidden Skill to fail")
	}
}

func TestResolveBuiltinSkillsSourceFindsProjectParent(t *testing.T) {
	root := t.TempDir()
	writeSyncTestSkill(t, filepath.Join(root, "backend", "skills", "worker", "review-flow"), "review-flow", "test body")

	nestedDir := filepath.Join(root, "backend", "cmd", "leros")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}

	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(nestedDir); err != nil {
		t.Fatalf("chdir nested dir: %v", err)
	}

	sourceDir, err := resolveBuiltinSkillsSource("", "worker")
	if err != nil {
		t.Fatalf("resolve builtin skills source: %v", err)
	}
	expected := filepath.Join(root, "backend", "skills", "worker")
	if resolvedExpected, evalErr := filepath.EvalSymlinks(expected); evalErr == nil {
		expected = resolvedExpected
	}
	if resolvedSource, evalErr := filepath.EvalSymlinks(sourceDir); evalErr == nil {
		sourceDir = resolvedSource
	}
	if sourceDir != expected {
		t.Fatalf("expected %s, got %s", expected, sourceDir)
	}
}

func writeSyncTestSkill(t *testing.T, dir string, name string, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + name + "\n---\n# " + name + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, skillManifestFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}
