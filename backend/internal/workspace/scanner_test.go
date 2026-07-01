package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitDiffIntegration(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Setup git repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = repoDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	// Set git user for the test repo (required for some git operations)
	exec.Command("git", "-C", repoDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoDir, "config", "user.name", "test").Run()

	os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("*.log\n"), 0o644)
	os.WriteFile(filepath.Join(repoDir, ".gitattributes"), []byte(""), 0o644)

	// Create initial files and commit so we have at least one commit.
	os.MkdirAll(filepath.Join(repoDir, "src"), 0o755)
	os.WriteFile(filepath.Join(repoDir, "src", "report.md"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(repoDir, ".env"), []byte("secret"), 0o644)   // should be ignored by builtin
	os.WriteFile(filepath.Join(repoDir, "test.log"), []byte("test"), 0o644) // .gitignore *.log
	os.WriteFile(filepath.Join(repoDir, "src", "keep.txt"), []byte("keep"), 0o644)

	// Create .leros dir (should be excluded by git diff fallback)
	lerosTurnDir := filepath.Join(repoDir, ".leros", "tasks", "1", "turns", "req1")
	os.MkdirAll(lerosTurnDir, 0o755)
	os.WriteFile(filepath.Join(lerosTurnDir, "artifacts.jsonl"), []byte(""), 0o644)

	// Initial commit to have a clean base
	exec.Command("git", "-C", repoDir, "add", ".").Run()
	exec.Command("git", "-C", repoDir, "commit", "-m", "initial").Run()

	// Capture pre-run tree
	ctx := context.Background()
	preTreeSHA, err := CapturePreRunTree(ctx, repoDir)
	if err != nil {
		t.Fatalf("CapturePreRunTree: %v", err)
	}
	if preTreeSHA == "" {
		t.Fatal("pre-run tree SHA should not be empty")
	}
	t.Logf("pre-run tree SHA: %s", preTreeSHA)

	// Simulate agent file changes: modify + create new
	os.WriteFile(filepath.Join(repoDir, "src", "report.md"), []byte("hello modified"), 0o644)
	os.WriteFile(filepath.Join(repoDir, "src", "newdoc.txt"), []byte("new content"), 0o644)

	// Capture post-run tree
	postTreeSHA, err := CapturePostRunTree(ctx, repoDir)
	if err != nil {
		t.Fatalf("CapturePostRunTree: %v", err)
	}
	if postTreeSHA == "" {
		t.Fatal("post-run tree SHA should not be empty")
	}
	t.Logf("post-run tree SHA: %s", postTreeSHA)

	// Test 1: DiffArtifacts should detect modified + new files
	entries, err := DiffArtifacts(ctx, repoDir, preTreeSHA, postTreeSHA)
	if err != nil {
		t.Fatalf("DiffArtifacts: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one changed file")
	}

	hasReport := false
	hasNewdoc := false
	for _, e := range entries {
		if e.Path == "src/report.md" {
			hasReport = true
		}
		if e.Path == "src/newdoc.txt" {
			hasNewdoc = true
		}
	}
	if !hasReport {
		t.Errorf("diff should contain src/report.md, got: %+v", entries)
	}
	if !hasNewdoc {
		t.Errorf("diff should contain src/newdoc.txt, got: %+v", entries)
	}

	// Test 2: .gitignored and .leros files should be excluded
	for _, e := range entries {
		if strings.HasPrefix(e.Path, ".leros/") || e.Path == ".leros" {
			t.Errorf("diff should NOT contain .leros/ files, got: %s", e.Path)
		}
		if e.Path == "test.log" {
			t.Errorf("diff should NOT contain test.log (gitignored), got: %s", e.Path)
		}
	}

	// Test 3: Same tree SHAs = empty diff
	sameEntries, err := DiffArtifacts(ctx, repoDir, preTreeSHA, preTreeSHA)
	if err != nil {
		t.Fatalf("DiffArtifacts with same SHA: %v", err)
	}
	if len(sameEntries) != 0 {
		t.Errorf("diff with same SHA should be empty, got: %+v", sameEntries)
	}

	t.Log("✓ Git diff integration test passed")
}

func TestGitDiffNoCommits(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	initCmd := exec.Command("git", "init")
	initCmd.Dir = repoDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}

	os.WriteFile(filepath.Join(repoDir, "untracked.txt"), []byte("untracked"), 0o644)

	ctx := context.Background()

	// git add --all with write-tree should work even without commits (uses empty tree as baseline)
	preTreeSHA, err := CapturePreRunTree(ctx, repoDir)
	if err != nil {
		t.Fatalf("CapturePreRunTree (no commits): %v", err)
	}
	if preTreeSHA == "" {
		t.Fatal("pre-run tree SHA should not be empty even without commits")
	}

	os.WriteFile(filepath.Join(repoDir, "newfile.txt"), []byte("new"), 0o644)

	postTreeSHA, err := CapturePostRunTree(ctx, repoDir)
	if err != nil {
		t.Fatalf("CapturePostRunTree (no commits): %v", err)
	}

	entries, err := DiffArtifacts(ctx, repoDir, preTreeSHA, postTreeSHA)
	if err != nil {
		t.Fatalf("DiffArtifacts (no commits): %v", err)
	}
	// newfile.txt should appear; untracked.txt was already in the pre-tree
	hasNew := false
	for _, e := range entries {
		if e.Path == "newfile.txt" {
			hasNew = true
		}
	}
	if !hasNew {
		t.Errorf("diff should contain newfile.txt (new untracked), got: %+v", entries)
	}

	t.Log("✓ Git diff no-commits test passed")
}

func TestManifestPriority(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	exec.Command("git", "-C", repoDir, "init").Run()
	exec.Command("git", "-C", repoDir, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", repoDir, "config", "user.name", "test").Run()

	os.WriteFile(filepath.Join(repoDir, "initial.txt"), []byte("initial"), 0o644)
	exec.Command("git", "-C", repoDir, "add", ".").Run()
	exec.Command("git", "-C", repoDir, "commit", "-m", "initial").Run()

	turnDir := filepath.Join(repoDir, ".leros", "tasks", "1", "turns", "req1")
	os.MkdirAll(turnDir, 0o755)
	manifestPath := filepath.Join(turnDir, "artifacts.jsonl")

	ctx := context.Background()
	preTreeSHA, _ := CapturePreRunTree(ctx, repoDir)

	// Simulate artifact_declare writing final entries to manifest
	os.WriteFile(manifestPath, []byte(`{"path":"explicit.txt","is_final":true}`+"\n"), 0o644)

	// Make some file changes that git diff would detect
	os.WriteFile(filepath.Join(repoDir, "implicit.txt"), []byte("implicit"), 0o644)

	plan := &TaskWorkspace{
		RepoDir:              repoDir,
		ArtifactManifestPath: manifestPath,
	}

	// Run GitDiffReconcile — should skip because explicit final entries exist
	if err := GitDiffReconcile(ctx, plan, preTreeSHA); err != nil {
		t.Fatalf("GitDiffReconcile: %v", err)
	}

	// Verify manifest only has the explicit entry, not the git diff entry
	data, _ := os.ReadFile(manifestPath)
	content := string(data)
	if strings.Contains(content, "implicit.txt") {
		t.Errorf("manifest should NOT contain implicit.txt (explicit entries take priority), got: %s", content)
	}
	if !strings.Contains(content, "explicit.txt") {
		t.Errorf("manifest should contain explicit.txt, got: %s", content)
	}

	t.Log("✓ Manifest priority test passed")
}
