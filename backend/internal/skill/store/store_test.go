package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestSkillStoreCreatePatchAndSupportingFiles(t *testing.T) {
	store, root := newTestStore(t)
	ctx := context.Background()

	result, err := store.Create(ctx, CreateRequest{
		Name:    "pr-review",
		Content: testSkillDocument("pr-review", "Review pull requests", "1. Read the diff.\n2. Return findings."),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected create success: %#v", result)
	}

	skillPath := filepath.Join(root, "pr-review", skillFileName)
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("expected skill file: %v", err)
	}

	patch, err := store.Patch(ctx, PatchRequest{
		Name:    "pr-review",
		OldText: "Return findings.",
		NewText: "Return findings ordered by severity.",
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if !patch.Success {
		t.Fatalf("expected patch success: %#v", patch)
	}

	body, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(body), "ordered by severity") {
		t.Fatalf("expected patched content, got:\n%s", string(body))
	}

	write, err := store.WriteFile(ctx, WriteFileRequest{
		Name:        "pr-review",
		FilePath:    "notes/checklists/release.md",
		FileContent: "check risk",
	})
	if err != nil {
		t.Fatalf("write file: %v", err)
	}
	if !write.Success {
		t.Fatalf("expected write success: %#v", write)
	}

	remove, err := store.RemoveFile(ctx, RemoveFileRequest{
		Name:     "pr-review",
		FilePath: "notes/checklists/release.md",
	})
	if err != nil {
		t.Fatalf("remove file: %v", err)
	}
	if !remove.Success {
		t.Fatalf("expected remove success: %#v", remove)
	}
}

func TestSkillStoreRejectsDuplicateSkillNames(t *testing.T) {
	store, _ := newTestStore(t)

	ctx := context.Background()
	if _, err := store.Create(ctx, CreateRequest{
		Name:    "debug-flow",
		Content: testSkillDocument("debug-flow", "Debug flow", "Steps."),
	}); err != nil {
		t.Fatalf("first create: %v", err)
	}

	result, err := store.Create(ctx, CreateRequest{
		Name:    "debug-flow",
		Content: testSkillDocument("debug-flow", "Debug flow", "Steps."),
	})
	if err != nil {
		t.Fatalf("duplicate create: %v", err)
	}
	if result.Success || !strings.Contains(result.Error, "already exists") {
		t.Fatalf("expected duplicate failure, got %#v", result)
	}
}

func TestSkillStorePatchRequiresUniqueMatch(t *testing.T) {
	store, _ := newTestStore(t)

	ctx := context.Background()
	if _, err := store.Create(ctx, CreateRequest{
		Name:    "repeat-flow",
		Content: testSkillDocument("repeat-flow", "Repeat flow", "same\nsame"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := store.Patch(ctx, PatchRequest{
		Name:    "repeat-flow",
		OldText: "same",
		NewText: "changed",
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if result.Success || !strings.Contains(result.Error, "multiple") {
		t.Fatalf("expected multiple match failure, got %#v", result)
	}
}

func TestSkillStoreRejectsUnsafeSupportingFilePath(t *testing.T) {
	store, _ := newTestStore(t)

	_, err := store.WriteFile(context.Background(), WriteFileRequest{
		Name:        "missing",
		FilePath:    "../escape.md",
		FileContent: "bad",
	})
	if err == nil {
		t.Fatalf("expected unsafe path error")
	}
}

func TestSkillStoreAllowsSupportingFilesAtAnySkillRelativePath(t *testing.T) {
	store, root := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, CreateRequest{
		Name:    "free-layout",
		Content: testSkillDocument("free-layout", "Free layout", "Steps."),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, filePath := range []string{"README.txt", "custom/deep/layout.txt"} {
		if _, err := store.WriteFile(ctx, WriteFileRequest{
			Name:        "free-layout",
			FilePath:    filePath,
			FileContent: filePath,
		}); err != nil {
			t.Fatalf("write %s: %v", filePath, err)
		}
		if _, err := os.Stat(filepath.Join(root, "free-layout", filepath.FromSlash(filePath))); err != nil {
			t.Fatalf("stat %s: %v", filePath, err)
		}
	}
}

func TestSkillStoreProtectsSkillDocumentAndGitInternals(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, CreateRequest{
		Name:    "protected-files",
		Content: testSkillDocument("protected-files", "Protected files", "Steps."),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, filePath := range []string{"SKILL.md", ".git/config", "nested/.GIT/config", "/tmp/escape"} {
		if _, err := store.WriteFile(ctx, WriteFileRequest{
			Name:        "protected-files",
			FilePath:    filePath,
			FileContent: "bad",
		}); err == nil {
			t.Fatalf("expected write rejection for %q", filePath)
		}
	}
	if _, err := store.RemoveFile(ctx, RemoveFileRequest{
		Name:     "protected-files",
		FilePath: "SKILL.md",
	}); err == nil {
		t.Fatal("expected SKILL.md removal rejection")
	}
}

func TestSkillStoreExplicitSkillDocumentPatchIsValidated(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, CreateRequest{
		Name:    "patch-document",
		Content: testSkillDocument("patch-document", "Patch document", "Steps."),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := store.Patch(ctx, PatchRequest{
		Name:     "patch-document",
		FilePath: "SKILL.md",
		OldText:  "name: patch-document",
		NewText:  "name: another-skill",
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if result.Success || result.ErrorCode != ErrDocumentInvalid.Code {
		t.Fatalf("expected invalid document result, got %#v", result)
	}
}

func TestSkillStoreRejectsSymbolicLinkEscape(t *testing.T) {
	store, root := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, CreateRequest{
		Name:    "safe-skill",
		Content: testSkillDocument("safe-skill", "Safe skill", "Steps."),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "safe-skill", "linked")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := store.WriteFile(ctx, WriteFileRequest{
		Name:        "safe-skill",
		FilePath:    "linked/escape.txt",
		FileContent: "bad",
	}); err == nil {
		t.Fatal("expected symbolic link escape rejection")
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file must not be written, got %v", err)
	}
}

func TestSkillStoreRejectsInvalidFrontmatter(t *testing.T) {
	store, _ := newTestStore(t)

	_, err := store.Create(context.Background(), CreateRequest{
		Name:    "bad-skill",
		Content: "# Missing frontmatter",
	})
	if err == nil {
		t.Fatalf("expected invalid frontmatter error")
	}
}

func TestSkillStoreRejectsDocumentNameMismatch(t *testing.T) {
	store, _ := newTestStore(t)

	if _, err := store.Create(context.Background(), CreateRequest{
		Name:    "directory-name",
		Content: testSkillDocument("other-name", "Mismatch", "Steps."),
	}); err == nil {
		t.Fatal("expected frontmatter name mismatch")
	}
}

func TestSkillStoreRejectsReservedRunsName(t *testing.T) {
	store, _ := newTestStore(t)

	if _, err := store.Create(context.Background(), CreateRequest{
		Name:    "runs",
		Content: testSkillDocument("runs", "Reserved", "Steps."),
	}); err == nil {
		t.Fatal("expected reserved name rejection")
	}
}

func TestDefaultSkillRootUsesWorkspaceRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	root, err := DefaultSkillRoot()
	if err != nil {
		t.Fatalf("default root: %v", err)
	}

	expected := filepath.Join(workspaceRoot, ".leros", "skills")
	if root != expected {
		t.Fatalf("expected %s, got %s", expected, root)
	}
}

func TestSkillStoreEditReplacesContent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, CreateRequest{
		Name:    "edit-test",
		Content: testSkillDocument("edit-test", "Original", "Original body."),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := store.Edit(ctx, EditRequest{
		Name:    "edit-test",
		Content: testSkillDocument("edit-test", "Updated", "Updated body."),
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected edit success: %#v", result)
	}

	// Verify content changed
	skill, err := store.Find(ctx, "edit-test")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(skill.Path, skillFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "Updated body") {
		t.Fatalf("expected updated content, got:\n%s", string(body))
	}
}

func TestSkillStoreDeleteRemovesDirectory(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, CreateRequest{
		Name:    "delete-test",
		Content: testSkillDocument("delete-test", "Delete me", "Body."),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	result, err := store.Delete(ctx, DeleteRequest{Name: "delete-test"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected delete success: %#v", result)
	}

	// Verify directory gone
	_, err = store.Find(ctx, "delete-test")
	if err == nil {
		t.Fatalf("expected skill to be deleted, but it still exists")
	}
}

func TestSkillStoreDeleteBaselineSkillWithoutChangingSeedManifest(t *testing.T) {
	store, root := newTestStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, CreateRequest{
		Name:    "builtin-skill",
		Content: testSkillDocument("builtin-skill", "Built-in", "Body."),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Write a .seed-manifest that includes this skill
	manifestPath := filepath.Join(root, ".seed-manifest")
	if err := os.WriteFile(manifestPath, []byte("builtin-skill:abc123def456\n"), 0o644); err != nil {
		t.Fatalf("write seed manifest: %v", err)
	}

	result, err := store.Delete(ctx, DeleteRequest{Name: "builtin-skill"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected local baseline deletion, got %#v", result)
	}

	if _, err := store.Find(ctx, "builtin-skill"); err == nil {
		t.Fatal("expected local skill directory to be deleted")
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read seed manifest: %v", err)
	}
	if string(manifest) != "builtin-skill:abc123def456\n" {
		t.Fatalf("seed manifest must remain baseline-owned, got %q", string(manifest))
	}
}

func TestSkillStoreForceInstallKeepsBaselineManifestOwnedByPreparer(t *testing.T) {
	store, root := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, CreateRequest{
		Name:    "baseline-skill",
		Content: testSkillDocument("baseline-skill", "Baseline", "Old body."),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	manifestPath := filepath.Join(root, ".seed-manifest")
	const manifestContent = "baseline-skill:abc123:7\n"
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); err != nil {
		t.Fatalf("write seed manifest: %v", err)
	}

	result, err := store.Install(ctx, InstallRequest{
		Name:    "baseline-skill",
		Content: testSkillDocument("baseline-skill", "Replacement", "New body."),
		Files: map[string]string{
			"custom/layout/info.txt": "supporting",
		},
		Force: true,
	})
	if err != nil {
		t.Fatalf("force install: %v", err)
	}
	if !result.Success {
		t.Fatalf("force install result: %#v", result)
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read seed manifest: %v", err)
	}
	if string(manifest) != manifestContent {
		t.Fatalf("seed manifest must remain unchanged, got %q", string(manifest))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read skill root: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".skill-install-") || strings.HasPrefix(entry.Name(), ".skill-backup-") {
			t.Fatalf("temporary directory was not cleaned: %s", entry.Name())
		}
	}
}

func newTestStore(t *testing.T) (*SkillStore, string) {
	t.Helper()

	home := t.TempDir()
	root := filepath.Join(home, "project-skills")
	t.Setenv("HOME", home)
	t.Setenv(leros.EnvWorkspaceRoot, filepath.Join(home, "workspace"))

	store, err := NewSkillStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store, root
}

func testSkillDocument(name string, description string, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\n" + body + "\n"
}
