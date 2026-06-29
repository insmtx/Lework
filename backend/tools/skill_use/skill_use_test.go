package skilluse

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/tools"
)

func TestSkillUseToolListAndGet(t *testing.T) {
	newTestCatalog(t)
	tool := NewSkillUseTool()

	rawListResult, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action": actionList,
	}))

	if err != nil {
		t.Fatalf("list skills failed: %v", err)
	}
	listResult := decodeSkillToolOutput(t, rawListResult)
	if listResult["success"] != true {
		t.Fatalf("expected successful list result, got %#v", listResult)
	}
	if listResult["count"] != float64(1) {
		t.Fatalf("expected 1 skill, got %#v", listResult["count"])
	}
	// Verify categories and hint
	categories, ok := listResult["categories"].([]interface{})
	if !ok || len(categories) != 1 || categories[0] != "github" {
		t.Fatalf("unexpected categories: %#v", listResult["categories"])
	}
	if listResult["hint"] != "Use skill_use with action=get and name=<skill_name> to see full content, tags, and linked files" {
		t.Fatalf("unexpected hint: %#v", listResult["hint"])
	}
	// Verify source and trust in first skill
	skills, ok := listResult["skills"].([]interface{})
	if !ok || len(skills) != 1 {
		t.Fatalf("unexpected skills: %#v", listResult["skills"])
	}
	skill0, ok := skills[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected skill entry type: %T", skills[0])
	}
	if skill0["source"] != "local" {
		t.Fatalf("expected source 'local', got %#v", skill0["source"])
	}
	if skill0["trust"] != "trusted" {
		t.Fatalf("expected trust 'trusted', got %#v", skill0["trust"])
	}

	rawGetResult, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action": actionGet,
		"name":   "GITHUB-PR-REVIEW",
	}))

	if err != nil {
		t.Fatalf("get skill failed: %v", err)
	}
	getResult := decodeSkillToolOutput(t, rawGetResult)

	if getResult["success"] != true {
		t.Fatalf("expected successful skill result, got %#v", getResult)
	}
	if getResult["name"] != "github-pr-review" {
		t.Fatalf("unexpected skill name: %#v", getResult["name"])
	}
	if getResult["content"] == "" {
		t.Fatalf("expected skill content")
	}
	files, ok := getResult["linked_files"].([]interface{})
	if !ok || len(files) != 2 || files[0] != "references/large.md" || files[1] != "references/policy.md" {
		t.Fatalf("unexpected linked files: %#v", getResult["linked_files"])
	}
	dir, ok := getResult["skill_dir"].(string)
	if !ok || !filepath.IsAbs(filepath.FromSlash(dir)) {
		t.Fatalf("expected skill_dir to be absolute, got %#v", getResult["skill_dir"])
	}
	for _, removedField := range []string{"related_skills", "scope", "skill_type", "enabled", "setup_needed", "readiness_status", "file_list_limit", "ok", "title", "output", "metadata", "skill", "body", "dir", "files"} {
		if _, exists := getResult[removedField]; exists {
			t.Fatalf("field %q should not be returned in skill view result: %#v", removedField, getResult[removedField])
		}
	}
}

func TestSkillUseToolReadFile(t *testing.T) {
	newTestCatalog(t)
	tool := NewSkillUseTool()

	rawResult, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action": actionReadFile,
		"name":   "github-pr-review",
		"path":   "references/policy.md",
	}))

	if err != nil {
		t.Fatalf("read skill file failed: %v", err)
	}
	result := decodeSkillToolOutput(t, rawResult)
	if result["success"] != true {
		t.Fatalf("expected successful read result, got %#v", result)
	}
	if result["content"] != "policy content" {
		t.Fatalf("unexpected file content: %#v", result["content"])
	}
	if result["size"] != float64(len("policy content")) {
		t.Fatalf("unexpected file size: %#v", result["size"])
	}
	if result["truncated"] != false {
		t.Fatalf("expected untruncated file, got %#v", result["truncated"])
	}
}

func TestSkillUseToolReadFileTruncatesLargeContent(t *testing.T) {
	newTestCatalog(t)
	tool := NewSkillUseTool()

	rawResult, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action": actionReadFile,
		"name":   "github-pr-review",
		"path":   "references/large.md",
	}))

	if err != nil {
		t.Fatalf("read large skill file failed: %v", err)
	}
	result := decodeSkillToolOutput(t, rawResult)
	if result["success"] != true {
		t.Fatalf("expected successful read result, got %#v", result)
	}
	if result["truncated"] != true {
		t.Fatalf("expected truncated file, got %#v", result["truncated"])
	}
	content, ok := result["content"].(string)
	if !ok {
		t.Fatalf("expected string content, got %#v", result["content"])
	}
	if len(content) != maxSkillFileReadBytes {
		t.Fatalf("expected content length %d, got %d", maxSkillFileReadBytes, len(content))
	}
}

func TestSkillUseToolLoadsBundledSkillByName(t *testing.T) {
	newBundledSkillsCatalog(t)
	tool := NewSkillUseTool()

	rawResult, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action": actionGet,
		"name":   "ANYSEARCH",
	}))

	if err != nil {
		t.Fatalf("get anysearch skill failed: %v", err)
	}
	result := decodeSkillToolOutput(t, rawResult)
	if result["success"] != true {
		t.Fatalf("expected successful anysearch skill result, got %#v", result)
	}

	if result["name"] != "anysearch" {
		t.Fatalf("unexpected skill name: %#v", result["name"])
	}

	content, ok := result["content"].(string)
	if !ok {
		t.Fatalf("expected anysearch skill content string, got %#v", result["content"])
	}
	if content == "" {
		t.Fatalf("expected non-empty anysearch skill content")
	}
}

func TestSkillUseToolMissingSkillReturnsAvailableNames(t *testing.T) {
	newTestCatalog(t)
	tool := NewSkillUseTool()

	rawResult, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action": actionGet,
		"name":   "missing",
	}))

	if err != nil {
		t.Fatalf("get missing skill should return structured result: %v", err)
	}
	result := decodeSkillToolOutput(t, rawResult)
	if result["success"] != false {
		t.Fatalf("expected not found result, got %#v", result)
	}

	available, ok := result["available_skills"].([]interface{})
	if !ok {
		t.Fatalf("expected available_skills, got %#v", result["available_skills"])
	}
	if len(available) != 1 || available[0] != "github-pr-review" {
		t.Fatalf("unexpected available skills: %#v", available)
	}

	hint, ok := result["hint"].(string)
	if !ok || hint == "" {
		t.Fatalf("expected non-empty hint, got %#v", result["hint"])
	}
}

func TestSkillUseToolGetSkillWithManifestMismatch(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, rootDir)

	skillsDir := filepath.Join(rootDir, ".leros", "skills")
	mismatchDir := filepath.Join(skillsDir, "mismatch-dir")
	if err := os.MkdirAll(mismatchDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// SKILL.md with a different name than the directory
	skillDocument := `---
name: different-name
description: A skill with mismatched name.
---
# Mismatch
`
	if err := os.WriteFile(filepath.Join(mismatchDir, "SKILL.md"), []byte(skillDocument), 0o644); err != nil {
		t.Fatalf("write skill failed: %v", err)
	}

	tool := NewSkillUseTool()
	rawResult, err := tool.Execute(context.Background(), tools.JSONInput(map[string]interface{}{
		"action": actionGet,
		"name":   "mismatch-dir",
	}))

	if err != nil {
		t.Fatalf("get mismatch skill should return structured result: %v", err)
	}
	result := decodeSkillToolOutput(t, rawResult)
	if result["success"] != false {
		t.Fatalf("expected not found result for manifest mismatch, got %#v", result)
	}

	errMsg, _ := result["error"].(string)
	if errMsg == "" || !strings.Contains(errMsg, "manifest name") {
		t.Fatalf("expected error to mention manifest name mismatch, got %q", errMsg)
	}

	available, ok := result["available_skills"].([]interface{})
	if !ok {
		t.Fatalf("expected available_skills in mismatch response, got %#v", result["available_skills"])
	}
	_ = available

	hint, ok := result["hint"].(string)
	if !ok || hint == "" {
		t.Fatalf("expected non-empty hint in mismatch response, got %#v", result["hint"])
	}
}

func TestSkillUseToolValidate(t *testing.T) {
	tool := NewSkillUseTool()

	if err := tool.Validate(tools.JSONInput(map[string]interface{}{})); err == nil {
		t.Fatalf("expected missing action to fail")
	}
	if err := tool.Validate(tools.JSONInput(map[string]interface{}{"action": actionGet})); err == nil {
		t.Fatalf("expected missing name to fail")
	}
	if err := tool.Validate(tools.JSONInput(map[string]interface{}{"action": "delete"})); err == nil {
		t.Fatalf("expected unsupported action to fail")
	}
}

func newTestCatalog(t *testing.T) {
	t.Helper()

	rootDir := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, rootDir)

	skillsDir := filepath.Join(rootDir, ".leros", "skills")
	skillDir := filepath.Join(skillsDir, "github-pr-review")
	referencesDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(referencesDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	skillDocument := `---
name: github-pr-review
description: Review GitHub pull requests.
version: 0.1.0
metadata:
  category: github
  tags: [github, pr, review]
  always: true
  requires_tools: [github.pr.get_files]
---
# Review

Read the pull request before reviewing.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillDocument), 0o644); err != nil {
		t.Fatalf("write skill failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(referencesDir, "policy.md"), []byte("policy content"), 0o644); err != nil {
		t.Fatalf("write reference failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(referencesDir, "large.md"), []byte(strings.Repeat("a", maxSkillFileReadBytes+5)), 0o644); err != nil {
		t.Fatalf("write large reference failed: %v", err)
	}
}

func decodeSkillToolOutput(t *testing.T, output string) map[string]interface{} {
	t.Helper()

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode skill tool output: %v\n%s", err, output)
	}
	return decoded
}

func newBundledSkillsCatalog(t *testing.T) {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve current test file")
	}

	// Bundled skills live at backend/skills/, but ScanSkillsDir() scans <workspace>/.leros/skills/.
	// Set workspace root to a temp dir and copy the bundled skills into .leros/skills/.
	sourceSkillsDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "skills", "worker")
	rootDir := t.TempDir()
	destSkillsDir := filepath.Join(rootDir, ".leros", "skills")
	if err := os.MkdirAll(filepath.Dir(destSkillsDir), 0o755); err != nil {
		t.Fatalf("mkdir .leros failed: %v", err)
	}
	// Copy the skills directory recursively via cp -a
	entries, err := os.ReadDir(sourceSkillsDir)
	if err != nil {
		t.Fatalf("read bundled skills dir: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		src := filepath.Join(sourceSkillsDir, entry.Name())
		dst := filepath.Join(destSkillsDir, entry.Name())
		if err := copyDir(src, dst); err != nil {
			t.Fatalf("copy skill %s: %v", entry.Name(), err)
		}
	}
	t.Setenv(leros.EnvWorkspaceRoot, rootDir)
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			content, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, content, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}
