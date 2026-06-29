package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildArtifactRecordNormalizesMimeType(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, "docs"), 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "docs", "report.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	record, err := BuildArtifactRecord(&TaskWorkspace{
		WorkspaceRoot: root,
		RepoDir:       repoDir,
	}, ManifestArtifact{
		Path:     "docs/report.txt",
		MimeType: "text/plain; charset=utf-8",
		IsFinal:  true,
	})
	if err != nil {
		t.Fatalf("BuildArtifactRecord failed: %v", err)
	}
	if record.MimeType != "text/plain" {
		t.Fatalf("mime type = %q, want text/plain", record.MimeType)
	}
	if record.OriginalName != "report.txt" {
		t.Fatalf("original name = %q, want report.txt", record.OriginalName)
	}
	if record.RelativePath != "docs/report.txt" {
		t.Fatalf("relative path = %q, want docs/report.txt", record.RelativePath)
	}
}
