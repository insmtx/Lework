package engines

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestSyncToLerosDirCreatesUserSkillsDirectory(t *testing.T) {
	builtinRoot := t.TempDir()
	lerosOSHome := t.TempDir()
	t.Setenv(leros.EnvHome, lerosOSHome)

	writeSyncTestSkill(t, filepath.Join(builtinRoot, "review-flow"), "review-flow", "test body")

	if err := SyncToLerosDir(builtinRoot); err != nil {
		t.Fatalf("sync to leros dir: %v", err)
	}

	// Verify the skill was synced to ~/.leros/skills
	userSkillsDir := filepath.Join(lerosOSHome, "skills", "review-flow")
	targetBody, err := os.ReadFile(filepath.Join(userSkillsDir, skillManifestFile))
	if err != nil {
		t.Fatalf("read synced skill: %v", err)
	}
	if string(targetBody) == "" {
		t.Fatal("expected skill content, got empty")
	}
}

func TestSyncFromLerosToExternalNoopsWhenNoSourcesExist(t *testing.T) {
	t.Setenv(leros.EnvHome, filepath.Join(t.TempDir(), "missing"))

	// Should not error when source doesn't exist
	if err := SyncFromLerosToExternal([]string{t.TempDir()}); err != nil {
		t.Fatalf("sync from missing sources should no-op: %v", err)
	}
}

func TestSyncFromLerosToExternalSkipsEmptyDirs(t *testing.T) {
	lerosOSHome := t.TempDir()
	t.Setenv(leros.EnvHome, lerosOSHome)

	// Create user skills directory (empty)
	userSkillsDir := filepath.Join(lerosOSHome, "skills")
	if err := os.MkdirAll(userSkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Should not error when no skills exist
	if err := SyncFromLerosToExternal([]string{t.TempDir()}); err != nil {
		t.Fatalf("sync with no skills should no-op: %v", err)
	}
}

func TestResolveBuiltinSkillsSourceFindsProjectParent(t *testing.T) {
	root := t.TempDir()
	writeSyncTestSkill(t, filepath.Join(root, "backend", "skills", "review-flow"), "review-flow", "test body")

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

	sourceDir, err := resolveBuiltinSkillsSource("")
	if err != nil {
		t.Fatalf("resolve builtin skills source: %v", err)
	}
	expected := filepath.Join(root, "backend", "skills")
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
