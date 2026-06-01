package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScannerIntegration(t *testing.T) {
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

	os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("*.log\n"), 0o644)

	// Create initial files
	os.MkdirAll(filepath.Join(repoDir, "src"), 0o755)
	os.WriteFile(filepath.Join(repoDir, "src", "report.md"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(repoDir, ".env"), []byte("secret"), 0o644)   // should be gitignored
	os.WriteFile(filepath.Join(repoDir, "test.log"), []byte("test"), 0o644) // .gitignore *.log

	// Create .leros dir (should be excluded)
	os.MkdirAll(filepath.Join(repoDir, ".leros", "tasks", "1", "turns", "req1"), 0o755)
	os.WriteFile(filepath.Join(repoDir, ".leros", "tasks", "1", "turns", "req1", "artifacts.jsonl"), []byte(""), 0o644)

	plan := &TaskWorkspace{
		RepoDir:              repoDir,
		BaselinePath:         filepath.Join(repoDir, ".leros", "tasks", "1", "turns", "req1", "baseline.jsonl"),
		ArtifactManifestPath: filepath.Join(repoDir, ".leros", "tasks", "1", "turns", "req1", "artifacts.jsonl"),
	}

	ctx := context.Background()

	// Test 1: WriteBaseline captures only non-ignored files
	if err := WriteBaseline(ctx, plan); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	data, err := os.ReadFile(plan.BaselinePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "src/report.md") {
		t.Errorf("baseline should contain src/report.md, got: %s", content)
	}
	if strings.Contains(content, ".git/") || strings.Contains(content, ".leros/") {
		t.Errorf("baseline should NOT contain .git/ or .leros/, got: %s", content)
	}
	if strings.Contains(content, "test.log") {
		t.Errorf("baseline should NOT contain test.log (gitignored), got: %s", content)
	}

	// Test 2: Modify file + add new file, then run Reconcile once.
	// The manifest is empty now (no final entries), so Reconcile should detect changes.
	os.WriteFile(filepath.Join(repoDir, "src", "report.md"), []byte("hello modified"), 0o644)
	os.WriteFile(filepath.Join(repoDir, "src", "newdoc.txt"), []byte("new content"), 0o644)
	if err := ReconcileArtifacts(ctx, plan); err != nil {
		t.Fatalf("ReconcileArtifacts: %v", err)
	}
	manifest, _ := os.ReadFile(plan.ArtifactManifestPath)
	manifestStr := string(manifest)
	if !strings.Contains(manifestStr, "src/report.md") {
		t.Errorf("manifest should contain src/report.md, got: %s", manifestStr)
	}
	if !strings.Contains(manifestStr, "src/newdoc.txt") {
		t.Errorf("manifest should contain src/newdoc.txt, got: %s", manifestStr)
	}

	// Test 3: After manifest has final entries, subsequent reconcile should skip.
	// Write a fresh baseline so diff gives new candidates.
	if err := WriteBaseline(ctx, plan); err != nil {
		t.Fatalf("WriteBaseline (reset): %v", err)
	}
	os.WriteFile(filepath.Join(repoDir, "src", "extra.txt"), []byte("extra"), 0o644)
	if err := ReconcileArtifacts(ctx, plan); err != nil {
		t.Fatalf("ReconcileArtifacts (skip): %v", err)
	}
	manifest3, _ := os.ReadFile(plan.ArtifactManifestPath)
	manifest3Str := string(manifest3)
	if strings.Contains(manifest3Str, "src/extra.txt") {
		t.Errorf("manifest should NOT contain extra.txt (reconcile skipped when final exists), got: %s", manifest3Str)
	}

	t.Log("✓ Integration test passed")
}
