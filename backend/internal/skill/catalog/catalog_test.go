package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestCatalogLoadsSkillDocuments(t *testing.T) {
	rootDir := t.TempDir()
	skillDir := filepath.Join(rootDir, "github-pr-review")
	referencesDir := filepath.Join(skillDir, "references")

	if err := os.MkdirAll(referencesDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	skillDocument := `---
name: github-pr-review
description: Review GitHub pull requests with Leros conventions.
version: 1.0.0
metadata:
  leros:
    category: github
    tags: [github, pr, review]
    always: true
    requires_tools: [github.pr.get_files, github.pr.publish_review]
---
# GitHub PR Review

Review steps here.
`
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(skillDocument), 0o644); err != nil {
		t.Fatalf("write skill failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(referencesDir, "policy.md"), []byte("policy content"), 0o644); err != nil {
		t.Fatalf("write reference failed: %v", err)
	}

	catalog, err := NewCatalog(os.DirFS(rootDir))
	if err != nil {
		t.Fatalf("load catalog failed: %v", err)
	}

	summaries := catalog.List()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 skill summary, got %d", len(summaries))
	}

	summary := summaries[0]
	if summary.Name != "github-pr-review" {
		t.Fatalf("expected skill name github-pr-review, got %s", summary.Name)
	}
	if !summary.Always {
		t.Fatalf("expected skill always flag to be true")
	}

	entry, err := catalog.Get("github-pr-review")
	if err != nil {
		t.Fatalf("get skill failed: %v", err)
	}
	if entry.Manifest.Metadata.Leros.Category != "github" {
		t.Fatalf("expected category github, got %s", entry.Manifest.Metadata.Leros.Category)
	}
	if entry.Body == "" {
		t.Fatalf("expected non-empty skill body")
	}

	referenceBody, err := catalog.ReadFile("github-pr-review", "references/policy.md")
	if err != nil {
		t.Fatalf("read skill file failed: %v", err)
	}
	if string(referenceBody) != "policy content" {
		t.Fatalf("unexpected reference body: %s", string(referenceBody))
	}

	files, err := catalog.ListFiles("github-pr-review", 10)
	if err != nil {
		t.Fatalf("list skill files failed: %v", err)
	}
	if len(files) != 1 || files[0] != "references/policy.md" {
		t.Fatalf("unexpected skill files: %#v", files)
	}
}

func TestCatalogDerivesNameWithoutFrontmatter(t *testing.T) {
	rootDir := t.TempDir()
	skillDir := filepath.Join(rootDir, "plain-skill")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte("# Plain Skill"), 0o644); err != nil {
		t.Fatalf("write skill failed: %v", err)
	}

	catalog, err := NewCatalog(os.DirFS(rootDir))
	if err != nil {
		t.Fatalf("load catalog failed: %v", err)
	}

	entry, err := catalog.Get("plain-skill")
	if err != nil {
		t.Fatalf("get skill failed: %v", err)
	}
	if entry.Manifest.Description != "plain-skill" {
		t.Fatalf("expected derived description plain-skill, got %s", entry.Manifest.Description)
	}
}

func TestCatalogFromDirStoresAbsoluteSkillDir(t *testing.T) {
	rootDir := t.TempDir()
	writeTestSkill(t, filepath.Join(rootDir, "review"), "review", "Review skill", "body")

	catalog, err := NewCatalogFromDir(rootDir)
	if err != nil {
		t.Fatalf("load catalog from dir: %v", err)
	}

	entry, err := catalog.Get("review")
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if !filepath.IsAbs(filepath.FromSlash(entry.AbsoluteDir)) {
		t.Fatalf("expected absolute skill dir, got %q", entry.AbsoluteDir)
	}
	expected := filepath.ToSlash(filepath.Join(rootDir, "review"))
	if entry.AbsoluteDir != expected {
		t.Fatalf("expected %q, got %q", expected, entry.AbsoluteDir)
	}
}

func TestCatalogMergeKeepsFirstDuplicateSkill(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeTestSkill(t, filepath.Join(firstRoot, "review"), "review", "First description", "first body")
	writeTestSkill(t, filepath.Join(secondRoot, "review"), "review", "Second description", "second body")

	first, err := NewCatalog(os.DirFS(firstRoot))
	if err != nil {
		t.Fatalf("load first catalog: %v", err)
	}
	second, err := NewCatalog(os.DirFS(secondRoot))
	if err != nil {
		t.Fatalf("load second catalog: %v", err)
	}

	merged := NewEmptyCatalog()
	merged.merge(first)
	merged.merge(second)

	entry, err := merged.Get("review")
	if err != nil {
		t.Fatalf("get merged skill: %v", err)
	}
	if entry.Manifest.Description != "First description" {
		t.Fatalf("expected first duplicate to win, got %q", entry.Manifest.Description)
	}
}

func TestDefaultLerosSkillsDirUsesWorkspaceRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	dir, err := defaultLerosSkillsDir()
	if err != nil {
		t.Fatalf("default workspace skills dir: %v", err)
	}

	expected := filepath.ToSlash(filepath.Join(workspaceRoot, ".leros", "skills"))
	if dir != expected {
		t.Fatalf("expected %s, got %s", expected, dir)
	}
}

func TestLoadDefaultCatalogCreatesMissingSkillsDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	catalog, rootDir, err := LoadDefaultCatalog()
	if err != nil {
		t.Fatalf("load default catalog: %v", err)
	}

	expected := filepath.ToSlash(filepath.Join(workspaceRoot, ".leros", "skills"))
	if rootDir != expected {
		t.Fatalf("expected root dir %s, got %s", expected, rootDir)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, ".leros", "skills")); err != nil {
		t.Fatalf("expected skills dir to be created: %v", err)
	}
	if summaries := catalog.List(); len(summaries) != 0 {
		t.Fatalf("expected empty catalog, got %#v", summaries)
	}
}

func TestCatalogRejectsPathTraversal(t *testing.T) {
	rootDir := t.TempDir()
	skillDir := filepath.Join(rootDir, "safe-skill")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte("# Safe Skill"), 0o644); err != nil {
		t.Fatalf("write skill failed: %v", err)
	}

	catalog, err := NewCatalog(os.DirFS(rootDir))
	if err != nil {
		t.Fatalf("load catalog failed: %v", err)
	}

	if _, err := catalog.ReadFile("safe-skill", "../secret.txt"); err == nil {
		t.Fatalf("expected traversal path to be rejected")
	}
}

func writeTestSkill(t *testing.T, dir string, name string, description string, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, skillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}
