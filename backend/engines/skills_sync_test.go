package engines

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestSyncToLerosDirCreatesUserSkillsDirectory(t *testing.T) {
	builtinRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	writeSyncTestSkill(t, filepath.Join(builtinRoot, "review-flow"), "review-flow", "test body")

	if err := SyncToLerosDir(builtinRoot); err != nil {
		t.Fatalf("sync to leros dir: %v", err)
	}

	userSkillsDir := filepath.Join(workspaceRoot, ".leros", "skills", "review-flow")
	targetBody, err := os.ReadFile(filepath.Join(userSkillsDir, skillManifestFile))
	if err != nil {
		t.Fatalf("read synced skill: %v", err)
	}
	if string(targetBody) == "" {
		t.Fatal("expected skill content, got empty")
	}
}

func TestReconcileExternalSkillLinksNoopsWhenNoSourcesExist(t *testing.T) {
	t.Setenv(leros.EnvWorkspaceRoot, filepath.Join(t.TempDir(), "missing"))

	if err := ReconcileExternalSkillLinks([]string{t.TempDir()}); err != nil {
		t.Fatalf("sync from missing sources should no-op: %v", err)
	}
}

func TestReconcileExternalSkillLinksSkipsEmptySkillsDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	userSkillsDir := filepath.Join(workspaceRoot, ".leros", "skills")
	if err := os.MkdirAll(userSkillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := ReconcileExternalSkillLinks([]string{t.TempDir()}); err != nil {
		t.Fatalf("sync with no skills should no-op: %v", err)
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
	// Normalize paths for macOS /var → /private/var symlink.
	resolvedExpected, err := filepath.EvalSymlinks(expected)
	if err == nil {
		expected = resolvedExpected
	}
	resolvedSource, err := filepath.EvalSymlinks(sourceDir)
	if err == nil {
		sourceDir = resolvedSource
	}
	if sourceDir != expected {
		t.Fatalf("expected %s, got %s", expected, sourceDir)
	}
}

func TestReconcileExternalSkillLinksCreatesExternalRootAndSymlinks(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	externalDir := filepath.Join(t.TempDir(), "cli-skills")
	if err := ReconcileExternalSkillLinks([]string{externalDir}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	target := filepath.Join(externalDir, "review-flow")
	fi, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat external symlink: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink, got non-symlink")
	}

	linkTarget, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	expected := filepath.Join(lerosSkills, "review-flow")
	if linkTarget != expected {
		t.Fatalf("expected symlink target %s, got %s", expected, linkTarget)
	}
}

func TestReconcileExternalSkillLinksRemovesExistingRealDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	externalDir := t.TempDir()
	realDir := filepath.Join(externalDir, "review-flow")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir real dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "old.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	if err := ReconcileExternalSkillLinks([]string{externalDir}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	fi, err := os.Lstat(filepath.Join(externalDir, "review-flow"))
	if err != nil {
		t.Fatalf("lstat after sync: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected real dir to be replaced by symlink")
	}
}

func TestReconcileExternalSkillLinksRemovesExistingFile(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	externalDir := t.TempDir()
	oldFile := filepath.Join(externalDir, "review-flow")
	if err := os.WriteFile(oldFile, []byte("old file"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}

	if err := ReconcileExternalSkillLinks([]string{externalDir}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	fi, err := os.Lstat(oldFile)
	if err != nil {
		t.Fatalf("lstat after sync: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected file to be replaced by symlink")
	}
}

func TestReconcileExternalSkillLinksReplacesWrongSymlink(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	externalDir := t.TempDir()
	wrongTarget := filepath.Join(t.TempDir(), "nowhere")
	wrongSymlink := filepath.Join(externalDir, "review-flow")
	if err := os.Symlink(wrongTarget, wrongSymlink); err != nil {
		t.Fatalf("create wrong symlink: %v", err)
	}

	if err := ReconcileExternalSkillLinks([]string{externalDir}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	linkTarget, err := os.Readlink(wrongSymlink)
	if err != nil {
		t.Fatalf("readlink after sync: %v", err)
	}
	expected := filepath.Join(lerosSkills, "review-flow")
	if linkTarget != expected {
		t.Fatalf("expected symlink target %s, got %s", expected, linkTarget)
	}
}

func TestReconcileExternalSkillLinksIdempotentOnCorrectSymlink(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	externalDir := t.TempDir()
	if err := ReconcileExternalSkillLinks([]string{externalDir}); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	if err := ReconcileExternalSkillLinks([]string{externalDir}); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	linkTarget, err := os.Readlink(filepath.Join(externalDir, "review-flow"))
	if err != nil {
		t.Fatalf("readlink after second sync: %v", err)
	}
	expected := filepath.Join(lerosSkills, "review-flow")
	if linkTarget != expected {
		t.Fatalf("expected symlink to remain %s, got %s", expected, linkTarget)
	}
}

func TestReconcileExternalSkillLinksNoopsWithNilDirs(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	if err := ReconcileExternalSkillLinks(nil); err != nil {
		t.Fatalf("sync with nil: %v", err)
	}
	if err := ReconcileExternalSkillLinks([]string{}); err != nil {
		t.Fatalf("sync with empty: %v", err)
	}
}

func TestReconcileExternalSkillLinksDefaultSkillPathIsLerosSkills(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	dir, err := defaultLerosSkillsDir()
	if err != nil {
		t.Fatalf("default workspace skills dir: %v", err)
	}

	expected := filepath.ToSlash(filepath.Join(workspaceRoot, ".leros", "skills"))
	if dir != filepath.ToSlash(expected) {
		t.Fatalf("expected %s, got %s", expected, dir)
	}
}

func TestEnsureExternalSkillLinkCreatesSymlink(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	externalDir := t.TempDir()
	if err := EnsureExternalSkillLink("review-flow", []string{externalDir}); err != nil {
		t.Fatalf("sync skill link: %v", err)
	}

	target := filepath.Join(externalDir, "review-flow")
	fi, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected symlink")
	}
	linkTarget, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if linkTarget != filepath.Join(lerosSkills, "review-flow") {
		t.Fatalf("wrong symlink target: %s", linkTarget)
	}
}

func TestEnsureExternalSkillLinkReplacesRealDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	externalDir := t.TempDir()
	realDir := filepath.Join(externalDir, "review-flow")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := EnsureExternalSkillLink("review-flow", []string{externalDir}); err != nil {
		t.Fatalf("sync skill link: %v", err)
	}

	fi, err := os.Lstat(filepath.Join(externalDir, "review-flow"))
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected real dir replaced by symlink")
	}
	}

func TestEnsureExternalSkillLinkRejectsEmptyName(t *testing.T) {
	if err := EnsureExternalSkillLink("", []string{t.TempDir()}); err == nil {
		t.Fatal("expected error for empty skill name")
	}
}

func TestRemoveExternalSkillLinkRemovesSymlink(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	lerosSkills := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSyncTestSkill(t, filepath.Join(lerosSkills, "review-flow"), "review-flow", "test body")

	externalDir := t.TempDir()

	// Create symlink first via EnsureExternalSkillLink.
	if err := EnsureExternalSkillLink("review-flow", []string{externalDir}); err != nil {
		t.Fatalf("EnsureExternalSkillLink: %v", err)
	}
	// Verify symlink was created.
	if _, err := os.Lstat(filepath.Join(externalDir, "review-flow")); err != nil {
		t.Fatalf("symlink should exist: %v", err)
	}

	// Remove the symlink.
	if err := RemoveExternalSkillLink("review-flow", []string{externalDir}); err != nil {
		t.Fatalf("RemoveExternalSkillLink: %v", err)
	}

	// Verify symlink was removed.
	if _, err := os.Lstat(filepath.Join(externalDir, "review-flow")); !os.IsNotExist(err) {
		t.Fatalf("symlink should be removed, got: %v", err)
	}
}

func TestRemoveExternalSkillLinkNoopsOnMissing(t *testing.T) {
	externalDir := t.TempDir()

	// Remove non-existent symlink should not error.
	if err := RemoveExternalSkillLink("nonexistent", []string{externalDir}); err != nil {
		t.Fatalf("RemoveExternalSkillLink should not error on missing: %v", err)
	}
}

func TestRemoveExternalSkillLinkPreservesRealDirectory(t *testing.T) {
	externalDir := t.TempDir()
	realDir := filepath.Join(externalDir, "my-skill")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// RemoveExternalSkillLink should not touch a real directory.
	if err := RemoveExternalSkillLink("my-skill", []string{externalDir}); err != nil {
		t.Fatalf("RemoveExternalSkillLink: %v", err)
	}

	// Verify real directory still exists.
	fi, err := os.Lstat(filepath.Join(externalDir, "my-skill"))
	if err != nil {
		t.Fatalf("real directory should still exist: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("expected real directory, not symlink")
	}
	if !fi.IsDir() {
		t.Fatal("expected directory")
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
