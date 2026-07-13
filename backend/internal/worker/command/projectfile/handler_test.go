package projectfile

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

type recordingPublisher struct {
	subject string
	data    []byte
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func (p *recordingPublisher) Publish(subject string, data []byte) error {
	p.subject = subject
	p.data = append([]byte(nil), data...)
	return nil
}

func TestHandleFileCommandRestoresAndCommitsFile(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	repoDir := filepath.Join(workspaceRoot, "projects", "1", "prj_test", "repo")
	filePath := filepath.Join(repoDir, "artifacts", "report.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("create artifact directory: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("current"), 0o644); err != nil {
		t.Fatalf("write current file: %v", err)
	}
	testRunGit(t, repoDir, "init", "-b", "main")
	testRunGit(t, repoDir, "add", "--all")
	testRunGit(t, repoDir, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-m", "initial")

	publisher := &recordingPublisher{}
	handler, err := New(publisher)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	handler.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("restored")),
			Header:     make(http.Header),
		}, nil
	})}
	command := messaging.NewProjectFileRestoreCommand(
		"restore-1",
		messaging.RouteContext{OrgID: 1, WorkerID: 2},
		messaging.ProjectFileRestoreCommandPayload{
			ProjectPublicID: "prj_test",
			RelativePath:    "artifacts/report.md",
			Branch:          "main",
			DownloadURL:     "http://storage.test/restored",
			AuthorName:      "test-user",
			AuthorEmail:     "test@example.com",
		},
	)
	command.Body.ReplyTo = "reply.restore"
	if err := handler.HandleFileCommand(context.Background(), command); err != nil {
		t.Fatalf("handle restore command: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(data) != "restored" {
		t.Fatalf("restored content = %q", data)
	}
	if publisher.subject != "reply.restore" {
		t.Fatalf("reply subject = %q", publisher.subject)
	}
	var result messaging.ProjectFileRestoreResult
	if err := json.Unmarshal(publisher.data, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.Success || result.CommitSHA == "" || result.RelativePath != "artifacts/report.md" {
		t.Fatalf("restore result = %#v", result)
	}
	if subject := testGitOutput(t, repoDir, "log", "-1", "--pretty=%s"); subject != "restore: artifacts/report.md" {
		t.Fatalf("commit subject = %q", subject)
	}
}

func testRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func testGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output[:len(output)-1])
}
