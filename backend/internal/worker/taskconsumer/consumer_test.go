package taskconsumer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/internal/agent"
	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

func TestIngestAttachmentsNoAttachments(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	req := &agent.RequestContext{
		Input: agent.InputContext{
			Attachments: nil,
		},
	}

	(&Consumer{}).ingestAttachments(context.Background(), req, nil)

	if _, err := os.Stat(filepath.Join(workspaceRoot, "uploads")); !os.IsNotExist(err) {
		t.Fatal("expected no uploads dir when there are no attachments")
	}
}

func TestIngestAttachmentsDownloadsToRepo(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	repoDir := filepath.Join(workspaceRoot, "testrepo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}

	gitInit(t, repoDir)

	plan := &agentworkspace.TaskWorkspace{
		RepoDir: repoDir,
	}

	req := &agent.RequestContext{
		Runtime: agent.RuntimeOptions{
			WorkDir: repoDir,
		},
		Input: agent.InputContext{
			Attachments: []agent.Attachment{
				{Name: "hello.txt", URL: srv.URL + "/hello.txt", MimeType: "text/plain"},
			},
		},
	}

	(&Consumer{}).ingestAttachments(context.Background(), req, plan)

	destPath := filepath.Join(repoDir, "uploads", "hello.txt")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("file content = %q, want %q", string(data), "hello")
	}

	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v: %s", err, string(out))
	}
	if !strings.Contains(string(out), "user attachment") {
		t.Fatalf("expected commit message containing 'user attachment', got: %s", string(out))
	}
}

func TestIngestAttachmentsTempWorkspaceNoGit(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("temp content"))
	}))
	defer srv.Close()

	workDir := filepath.Join(workspaceRoot, "temp")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create work dir: %v", err)
	}

	req := &agent.RequestContext{
		Runtime: agent.RuntimeOptions{
			WorkDir: workDir,
		},
		Input: agent.InputContext{
			Attachments: []agent.Attachment{
				{Name: "file.txt", URL: srv.URL + "/file.txt", MimeType: "text/plain"},
			},
		},
	}

	(&Consumer{}).ingestAttachments(context.Background(), req, nil)

	destPath := filepath.Join(workDir, "uploads", "file.txt")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "temp content" {
		t.Fatalf("file content = %q, want %q", string(data), "temp content")
	}

	if _, err := os.Stat(filepath.Join(workDir, ".git")); !os.IsNotExist(err) {
		t.Fatal("expected no .git dir in temp workspace")
	}
}

func TestIngestAttachmentsSkipDownloadFailure(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	validSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer validSrv.Close()

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	repoDir := filepath.Join(workspaceRoot, "testrepo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	gitInit(t, repoDir)

	plan := &agentworkspace.TaskWorkspace{
		RepoDir: repoDir,
	}

	req := &agent.RequestContext{
		Runtime: agent.RuntimeOptions{
			WorkDir: repoDir,
		},
		Input: agent.InputContext{
			Attachments: []agent.Attachment{
				{Name: "good.txt", URL: validSrv.URL + "/good.txt", MimeType: "text/plain"},
				{Name: "bad.txt", URL: failSrv.URL + "/bad.txt", MimeType: "text/plain"},
			},
		},
	}

	(&Consumer{}).ingestAttachments(context.Background(), req, plan)

	goodPath := filepath.Join(repoDir, "uploads", "good.txt")
	if _, err := os.Stat(goodPath); os.IsNotExist(err) {
		t.Fatal("expected good.txt to be downloaded")
	}

	badPath := filepath.Join(repoDir, "uploads", "bad.txt")
	if _, err := os.Stat(badPath); !os.IsNotExist(err) {
		t.Fatal("expected bad.txt not to be created on download failure")
	}
}

func TestIngestAttachmentsSkipEmptyURLOrName(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)

	workDir := filepath.Join(workspaceRoot, "temp")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create work dir: %v", err)
	}

	req := &agent.RequestContext{
		Runtime: agent.RuntimeOptions{
			WorkDir: workDir,
		},
		Input: agent.InputContext{
			Attachments: []agent.Attachment{
				{Name: "no_url.txt", URL: "", MimeType: "text/plain"},
				{Name: "", URL: "http://example.com/unnamed", MimeType: "text/plain"},
				{Name: "both_empty", URL: "", MimeType: ""},
			},
		},
	}

	(&Consumer{}).ingestAttachments(context.Background(), req, nil)

	entries, err := os.ReadDir(filepath.Join(workDir, "uploads"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read uploads dir: %v", err)
	}
	if len(entries) > 0 {
		t.Fatalf("expected no files, got %d", len(entries))
	}
}

func gitInit(t *testing.T, repoDir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", args[0], err, string(out))
		}
	}
}

func TestDownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "testfile.txt")

	if err := downloadFile(context.Background(), srv.URL+"/testfile.txt", destPath); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("content = %q, want %q", string(data), "hello world")
	}
}

func TestDownloadFileHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "should_not_exist")

	err := downloadFile(context.Background(), srv.URL+"/missing", destPath)
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error, got: %v", err)
	}
}

func TestDownloadFileLargeContent(t *testing.T) {
	content := make([]byte, 10*1024*1024) // 10MB
	for i := range content {
		content[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10485760")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "large.bin")

	if err := downloadFile(context.Background(), srv.URL+"/large.bin", destPath); err != nil {
		t.Fatalf("downloadFile: %v", err)
	}

	f, err := os.Open(destPath)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer f.Close()
	buf := make([]byte, len(content))
	if _, err := io.ReadFull(f, buf); err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(buf) != string(content) {
		t.Fatal("content mismatch")
	}
}
