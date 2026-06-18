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
		FilePath:    "references/checklist.md",
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
		FilePath: "references/checklist.md",
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

func TestSkillStoreDeleteRejectsSeedSkill(t *testing.T) {
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
	manifestPath := filepath.Join(root, seedManifestFile)
	if err := os.WriteFile(manifestPath, []byte("builtin-skill:abc123def456\n"), 0o644); err != nil {
		t.Fatalf("write seed manifest: %v", err)
	}

	// Attempt delete — must be rejected
	result, err := store.Delete(ctx, DeleteRequest{Name: "builtin-skill"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if result.Success {
		t.Fatalf("expected delete to be rejected for seed skill")
	}
	if result.ErrorCode != ErrSkillIsBuiltin.Code {
		t.Fatalf("expected error code %q, got %q", ErrSkillIsBuiltin.Code, result.ErrorCode)
	}

	// Verify skill still exists
	skill, err := store.Find(ctx, "builtin-skill")
	if err != nil {
		t.Fatalf("expected seed skill to still exist after rejected delete: %v", err)
	}
	if skill == nil {
		t.Fatal("expected seed skill to still exist")
	}
}

func TestSkillStoreDeleteCaseInsensitiveSeedCheck(t *testing.T) {
	store, root := newTestStore(t)
	ctx := context.Background()

	_, err := store.Create(ctx, CreateRequest{
		Name:    "my-skill",
		Content: testSkillDocument("my-skill", "My Skill", "Body."),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Seed manifest has uppercase skill name — should still match case-insensitively
	manifestPath := filepath.Join(root, seedManifestFile)
	if err := os.WriteFile(manifestPath, []byte("MY-SKILL:abc123def456\n"), 0o644); err != nil {
		t.Fatalf("write seed manifest: %v", err)
	}

	result, err := store.Delete(ctx, DeleteRequest{Name: "my-skill"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if result.Success {
		t.Fatal("expected case-insensitive seed check to block delete")
	}
	if result.ErrorCode != ErrSkillIsBuiltin.Code {
		t.Fatalf("expected error code %q, got %q", ErrSkillIsBuiltin.Code, result.ErrorCode)
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
