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

	// Create .lework dir (should be excluded by git diff fallback)
	leworkTurnDir := filepath.Join(repoDir, ".lework", "tasks", "1", "turns", "req1")
	os.MkdirAll(leworkTurnDir, 0o755)
	os.WriteFile(filepath.Join(leworkTurnDir, "artifacts.jsonl"), []byte(""), 0o644)

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

	// Test 2: .gitignored and .lework files should be excluded
	for _, e := range entries {
		if strings.HasPrefix(e.Path, ".lework/") || e.Path == ".lework" {
			t.Errorf("diff should NOT contain .lework/ files, got: %s", e.Path)
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

func TestDiffArtifactsDetectsRenamePaths(t *testing.T) {
	repoDir := t.TempDir()
	runGitTestCommand(t, repoDir, "init")
	runGitTestCommand(t, repoDir, "config", "user.email", "test@test.com")
	runGitTestCommand(t, repoDir, "config", "user.name", "test")

	oldPath := filepath.Join(repoDir, "reports", "old-name.txt")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatalf("create reports directory: %v", err)
	}
	if err := os.WriteFile(oldPath, []byte("same content"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	runGitTestCommand(t, repoDir, "add", ".")
	runGitTestCommand(t, repoDir, "commit", "-m", "initial")

	preTreeSHA, err := CapturePreRunTree(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("capture pre-run tree: %v", err)
	}
	newPath := filepath.Join(repoDir, "reports", "new-name.txt")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("rename file: %v", err)
	}
	postTreeSHA, err := CapturePostRunTree(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("capture post-run tree: %v", err)
	}

	entries, err := DiffArtifacts(context.Background(), repoDir, preTreeSHA, postTreeSHA)
	if err != nil {
		t.Fatalf("diff artifacts: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("rename entries = %#v, want one", entries)
	}
	if entries[0].Path != "reports/new-name.txt" || entries[0].PreviousPath != "reports/old-name.txt" {
		t.Fatalf("rename entry = %#v", entries[0])
	}
}

func runGitTestCommand(t *testing.T, repoDir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
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

	turnDir := filepath.Join(repoDir, ".lework", "tasks", "1", "turns", "req1")
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

func TestGitDiffReconcileEnrichesExplicitRename(t *testing.T) {
	repoDir := t.TempDir()
	runGitTestCommand(t, repoDir, "init")
	runGitTestCommand(t, repoDir, "config", "user.email", "test@test.com")
	runGitTestCommand(t, repoDir, "config", "user.name", "test")

	oldPath := filepath.Join(repoDir, "old-name.txt")
	if err := os.WriteFile(oldPath, []byte("same content"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	runGitTestCommand(t, repoDir, "add", ".")
	runGitTestCommand(t, repoDir, "commit", "-m", "initial")

	ctx := context.Background()
	preTreeSHA, err := CapturePreRunTree(ctx, repoDir)
	if err != nil {
		t.Fatalf("capture pre-run tree: %v", err)
	}
	newPath := filepath.Join(repoDir, "new-name.txt")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("rename file: %v", err)
	}

	turnDir := filepath.Join(repoDir, ".lework", "tasks", "1", "turns", "req1")
	if err := os.MkdirAll(turnDir, 0o755); err != nil {
		t.Fatalf("create turn directory: %v", err)
	}
	manifestPath := filepath.Join(turnDir, "artifacts.jsonl")
	if err := os.WriteFile(
		manifestPath,
		[]byte(`{"path":"new-name.txt","title":"Renamed report","is_final":true,"source":"agent_declared"}`+"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	plan := &TaskWorkspace{RepoDir: repoDir, ArtifactManifestPath: manifestPath}
	if err := GitDiffReconcile(ctx, plan, preTreeSHA); err != nil {
		t.Fatalf("reconcile rename: %v", err)
	}

	records, err := CollectFinalArtifacts(ctx, plan)
	if err != nil {
		t.Fatalf("collect artifacts: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %#v, want one", records)
	}
	if records[0].RelativePath != "new-name.txt" || records[0].PreviousPath != "old-name.txt" {
		t.Fatalf("renamed record = %#v", records[0])
	}
	if records[0].Title != "Renamed report" || records[0].Source != "agent_declared" {
		t.Fatalf("explicit metadata was not preserved: %#v", records[0])
	}
}

func TestGitDiffReconcileFiltersAttendanceWorkFiles(t *testing.T) {
	repoDir := t.TempDir()
	runGitTestCommand(t, repoDir, "init")
	runGitTestCommand(t, repoDir, "config", "user.email", "test@test.com")
	runGitTestCommand(t, repoDir, "config", "user.name", "test")
	write := func(path string, content []byte) {
		t.Helper()
		full := filepath.Join(repoDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("seed.txt", []byte("seed"))
	write(".gitignore", []byte(".lework/\n"))
	runGitTestCommand(t, repoDir, "add", ".")
	runGitTestCommand(t, repoDir, "commit", "-m", "initial")

	ctx := context.Background()
	preTree, err := CapturePreRunTree(ctx, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	write(".attendance-work/person-matches.json", []byte("internal"))
	write(".attendance-pages/doc-1/page-1.png", []byte("image"))
	write("考勤确认表.md", []byte("attendance review"))
	write("2026年6月核算表.xlsx", []byte("workbook"))
	write("ordinary-report.md", []byte("ordinary diff"))
	turnDir := filepath.Join(repoDir, ".lework", "tasks", "1", "turns", "req1")
	if err := os.MkdirAll(turnDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(turnDir, "artifacts.jsonl")
	plan := &TaskWorkspace{RepoDir: repoDir, ArtifactManifestPath: manifestPath}
	if err := GitDiffReconcile(ctx, plan, preTree); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, internalPath := range []string{".attendance-work/", ".attendance-pages/"} {
		if strings.Contains(string(manifest), internalPath) {
			t.Fatalf("diff manifest exposes internal path %q: %s", internalPath, manifest)
		}
	}
	if !strings.Contains(string(manifest), "ordinary-report.md") {
		t.Fatalf("ordinary source:diff artifact was lost: %s", manifest)
	}

	// Even explicit final declarations for private work paths are not delivered.
	write(".attendance-work/attendance.json", []byte("private"))
	write(".attendance-pages/doc-1/page-1.json", []byte("private"))
	write("考勤确认表.md", []byte("attendance review"))
	write("2026年6月核算表.xlsx", []byte("workbook"))
	manifest = []byte(
		`{"path":".attendance-work/attendance.json","is_final":true}` + "\n" +
			`{"path":".attendance-pages/doc-1/page-1.json","is_final":true}` + "\n" +
			`{"path":"考勤确认表.md","is_final":true}` + "\n" +
			`{"path":"考勤确认表.md","is_final":true}` + "\n" +
			`{"path":"2026年6月核算表.xlsx","is_final":true}` + "\n",
	)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	records, err := CollectFinalArtifacts(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("collected records = %#v, want two formal files with duplicate declaration deduplicated", records)
	}
	seen := map[string]bool{}
	for _, record := range records {
		seen[record.RelativePath] = true
	}
	if !seen["考勤确认表.md"] || !seen["2026年6月核算表.xlsx"] {
		t.Fatalf("formal business files missing: %#v", records)
	}
	for _, path := range []string{".attendance-work/attendance.json", ".attendance-pages/doc-1/page-1.json"} {
		if IsAttendanceInternalPath(path) != true {
			t.Fatalf("expected private path to be recognized: %s", path)
		}
		if _, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(path))); err != nil {
			t.Fatalf("filtering removed internal file %s: %v", path, err)
		}
	}
}

func TestGitDiffReconcileFallsBackForOnlyInternalFinalDeclarations(t *testing.T) {
	repoDir := t.TempDir()
	runGitTestCommand(t, repoDir, "init")
	runGitTestCommand(t, repoDir, "config", "user.email", "test@test.com")
	runGitTestCommand(t, repoDir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repoDir, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte(".lework/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repoDir, "add", ".")
	runGitTestCommand(t, repoDir, "commit", "-m", "initial")

	ctx := context.Background()
	preTree, err := CapturePreRunTree(ctx, repoDir)
	if err != nil {
		t.Fatal(err)
	}
	internalPath := filepath.Join(repoDir, ".attendance-work", "person-matches.json")
	if err := os.MkdirAll(filepath.Dir(internalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(internalPath, []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "ordinary-report.md"), []byte("report"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(repoDir, ".lework", "tasks", "1", "turns", "req1", "artifacts.jsonl")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"path":".attendance-work/person-matches.json","is_final":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &TaskWorkspace{RepoDir: repoDir, ArtifactManifestPath: manifestPath}
	if err := GitDiffReconcile(ctx, plan, preTree); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"path":"ordinary-report.md"`) || !strings.Contains(string(manifest), `"source":"diff"`) {
		t.Fatalf("internal-only declaration suppressed ordinary diff fallback: %s", manifest)
	}
	artifacts, err := CollectFinalArtifacts(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].RelativePath != "ordinary-report.md" {
		t.Fatalf("final artifacts = %#v, want only ordinary report", artifacts)
	}
	if _, err := os.Stat(internalPath); err != nil {
		t.Fatalf("filtering removed internal source file: %v", err)
	}
}

func TestAttendanceArtifactPathFilteringBoundaries(t *testing.T) {
	for _, path := range []string{
		".attendance-work/file.json",
		"./.attendance-work/file.json",
		".attendance-work/subdir/file.json",
		".attendance-work/../.attendance-work/file.json",
		`.attendance-work\subdir\file.json`,
		`.attendance-pages//page.json`,
	} {
		if !IsAttendanceInternalPath(path) {
			t.Errorf("IsAttendanceInternalPath(%q) = false", path)
		}
	}
	for _, path := range []string{"output/.attendance-work/file.json", ".attendance-work-copy/file.json", "ordinary-report.md"} {
		if IsAttendanceInternalPath(path) {
			t.Errorf("IsAttendanceInternalPath(%q) = true", path)
		}
	}
}

func TestFinalArtifactDeclarationsRejectInvalidPathsAndMalformedManifest(t *testing.T) {
	for _, path := range []string{"", ".", "../outside.json", "/absolute.json", `C:\outside.json`} {
		t.Run(path, func(t *testing.T) {
			_, eligible, err := effectiveFinalArtifactPath(t.TempDir(), ManifestArtifact{Path: path, IsFinal: true})
			if err == nil || eligible {
				t.Fatalf("invalid path %q accepted: eligible=%v err=%v", path, eligible, err)
			}
		})
	}
	manifestPath := filepath.Join(t.TempDir(), "artifacts.jsonl")
	if err := os.WriteFile(manifestPath, []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manifestFinalPaths(manifestPath, t.TempDir()); err == nil {
		t.Fatal("malformed manifest was silently accepted for diff fallback")
	}
}

func TestCollectFinalArtifactsRejectsExternalSymlink(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(root, "outside.md")
	if err := os.WriteFile(external, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(repoDir, "report.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	manifestPath := filepath.Join(root, "artifacts.jsonl")
	if err := os.WriteFile(manifestPath, []byte(`{"path":"report.md","is_final":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manifestFinalPaths(manifestPath, repoDir); err == nil {
		t.Fatal("external symlink declaration was accepted as an effective final path")
	}
	if _, err := CollectFinalArtifacts(context.Background(), &TaskWorkspace{RepoDir: repoDir, ArtifactManifestPath: manifestPath}); err == nil {
		t.Fatal("external symlink was accepted as a final artifact")
	}
}
