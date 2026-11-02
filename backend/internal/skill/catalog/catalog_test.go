package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/insmtx/Lework/backend/pkg/lework"
)

func TestList(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "review", "Review skill", "review body")

	summaries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(summaries))
	}
	s := summaries[0]
	if s.Name != "review" {
		t.Fatalf("expected name review, got %s", s.Name)
	}
	if s.Description != "Review skill" {
		t.Fatalf("expected description 'Review skill', got %s", s.Description)
	}
}

func TestListDerivesNameWithoutFrontmatter(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	skillDir := filepath.Join(skillsDir, "plain-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte("# Plain Skill"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	summaries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(summaries))
	}
	if summaries[0].Name != "plain-skill" {
		t.Fatalf("expected derived name plain-skill, got %s", summaries[0].Name)
	}
}

func TestListCreatesMissingDirAndScansEmpty(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(lework.EnvWorkspaceRoot, workspaceRoot)

	summaries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	expected := filepath.Join(workspaceRoot, ".lework", "skills")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected skills dir to be created at %s: %v", expected, err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected empty catalog, got %d skills", len(summaries))
	}
}

func TestListWithFrontmatterMetadata(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	skillDir := filepath.Join(skillsDir, "github-pr-review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	skillDocument := `---
name: github-pr-review
description: Review GitHub pull requests.
version: 1.0.0
metadata:
  category: github
  tags: [github, pr]
  always: true
  requires_tools: [github.pr.get_files]
---
# GitHub PR Review
`
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(skillDocument), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	summaries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(summaries))
	}

	s := summaries[0]
	if s.Name != "github-pr-review" {
		t.Fatalf("expected name github-pr-review, got %s", s.Name)
	}
	if !s.Always {
		t.Fatalf("expected always=true")
	}
	if s.Category != "github" {
		t.Fatalf("expected category github, got %s", s.Category)
	}
	if s.Version != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", s.Version)
	}
}

func TestListUsesStoredSkillMetadataSource(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	skillDir := filepath.Join(skillsDir, "market-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillFileName), []byte(`---
name: market-skill
description: Installed from marketplace.
---
# Market Skill
`), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, skillMetadataFileName), []byte(`{"source":"Lework","skill_id":"market-skill"}`), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	summaries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(summaries))
	}
	if summaries[0].Source != "Lework" {
		t.Fatalf("expected source Lework, got %s", summaries[0].Source)
	}
	if summaries[0].SkillID != "market-skill" {
		t.Fatalf("expected skill id market-skill, got %s", summaries[0].SkillID)
	}
}

func TestGetCaseInsensitive(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "my-skill", "My Skill", "body")

	entry, err := Get("MY-SKILL")
	if err != nil {
		t.Fatalf("Get MY-SKILL: %v", err)
	}
	if entry.Manifest.Name != "my-skill" {
		t.Fatalf("expected my-skill, got %s", entry.Manifest.Name)
	}
}

func TestGetNotFound(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "review", "Review", "body")

	if _, err := Get("nonexistent"); err == nil {
		t.Fatalf("expected error for nonexistent skill")
	}
}

func TestGetManifestMismatchDirectPath(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	// Create a directory named "mismatch-dir" but the SKILL.md manifest has a different name.
	mismatchDir := filepath.Join(skillsDir, "mismatch-dir")
	if err := os.MkdirAll(mismatchDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	skillDocument := `---
name: different-name
description: A skill with mismatched name.
---
# Mismatch
`
	if err := os.WriteFile(filepath.Join(mismatchDir, skillFileName), []byte(skillDocument), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	// Looking up by the directory name should fail with manifest name mismatch.
	if _, err := Get("mismatch-dir"); err == nil {
		t.Fatalf("expected mismatch error for mismatch-dir")
	}

	// Looking up by the manifest name should fail since no directory matches that name.
	if _, err := Get("different-name"); err == nil {
		t.Fatalf("expected different-name to not be found (dir is mismatch-dir)")
	}
}

func TestReadFile(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	skillDir := filepath.Join(skillsDir, "review")
	referencesDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(referencesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTestSkill(t, skillsDir, "review", "Review skill", "body")
	if err := os.WriteFile(filepath.Join(referencesDir, "policy.md"), []byte("policy content"), 0o644); err != nil {
		t.Fatalf("write reference: %v", err)
	}

	content, err := ReadFile("review", "references/policy.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "policy content" {
		t.Fatalf("expected 'policy content', got %q", string(content))
	}
}

func TestReadFileRejectsPathTraversal(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	writeTestSkill(t, skillsDir, "safe-skill", "Safe", "body")

	if _, err := ReadFile("safe-skill", "../secret.txt"); err == nil {
		t.Fatalf("expected traversal path to be rejected")
	}
}

func TestListFiles(t *testing.T) {
	skillsDir := setupSkillsRoot(t)
	skillDir := filepath.Join(skillsDir, "review")
	referencesDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(referencesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTestSkill(t, skillsDir, "review", "Review skill", "body")
	if err := os.WriteFile(filepath.Join(referencesDir, "policy.md"), []byte("policy"), 0o644); err != nil {
		t.Fatalf("write reference: %v", err)
	}
	if err := os.WriteFile(filepath.Join(referencesDir, "guide.md"), []byte("guide"), 0o644); err != nil {
		t.Fatalf("write reference: %v", err)
	}

	files, err := ListFiles("review", 10)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
	if files[0] != "references/guide.md" || files[1] != "references/policy.md" {
		t.Fatalf("unexpected files: %v", files)
	}
}

func setupSkillsRoot(t *testing.T) string {
	t.Helper()
	rootDir := t.TempDir()
	t.Setenv(lework.EnvWorkspaceRoot, rootDir)
	skillsDir := filepath.Join(rootDir, ".lework", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	return skillsDir
}

func writeTestSkill(t *testing.T, skillsDir string, name string, description string, body string) {
	t.Helper()
	dir := filepath.Join(skillsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, skillFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}
