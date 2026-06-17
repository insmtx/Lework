package gitea_test

import (
	"context"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/internal/infra/gitea"
)

const (
	testEndpoint   = "http://49.232.218.218:3009"
	testToken      = "806372856159056499ffdc289d3238763d27c993"
	testOwner      = "leros-system"
	testOrgPrefix  = "leros"
	testEnv        = "test"
)

func newTestClient(t *testing.T) *gitea.Client {
	t.Helper()
	return gitea.NewClient(testEndpoint, testToken)
}

func TestClient_CreateAndDeleteRepo(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoName := "test-create-" + time.Now().Format("200601021504")
	repo, err := client.CreateRepo(ctx, testOwner, gitea.CreateRepoRequest{
		Name:        repoName,
		Description: "integration test repo",
		Private:     true,
		AutoInit:    true,
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if repo.FullName != testOwner+"/"+repoName {
		t.Errorf("expected full_name %s/%s, got %s", testOwner, repoName, repo.FullName)
	}
	t.Logf("created repo: %s (id=%d, clone_url=%s)", repo.FullName, repo.ID, repo.CloneURL)

	if err := client.DeleteRepo(ctx, testOwner, repoName); err != nil {
		t.Errorf("delete repo: %v", err)
	}
}

func TestClient_GetRepo(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := client.GetRepo(ctx, testOwner, "test-get-"+time.Now().Format("1504"))
	if err != nil {
		t.Logf("repo not found (expected for non-existent repo): %v", err)
	}
}

func TestClient_ListContents(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoName := "test-contents-" + time.Now().Format("200601021504")
	_, err := client.CreateRepo(ctx, testOwner, gitea.CreateRepoRequest{
		Name:        repoName,
		Description: "test contents",
		Private:     true,
		AutoInit:    true,
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	defer client.DeleteRepo(ctx, testOwner, repoName)

	entries, err := client.ListContents(ctx, testOwner, repoName, "", "main")
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	t.Logf("root entries: %d", len(entries))
	for _, e := range entries {
		t.Logf("  %s (type=%s, size=%d)", e.Name, e.Type, e.Size)
	}
}

func TestClient_CreateFileAndGetRaw(t *testing.T) {
	client := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repoName := "test-file-" + time.Now().Format("200601021504")
	_, err := client.CreateRepo(ctx, testOwner, gitea.CreateRepoRequest{
		Name:        repoName,
		Description: "test file read",
		Private:     true,
		AutoInit:    true,
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	defer client.DeleteRepo(ctx, testOwner, repoName)

	err = client.CreateFile(ctx, testOwner, repoName, "hello.md",
		"SGVsbG8gV29ybGQ=", "add hello.md")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	reader, err := client.GetRawFile(ctx, testOwner, repoName, "main", "hello.md")
	if err != nil {
		t.Fatalf("get raw file: %v", err)
	}
	defer reader.Close()

	buf := make([]byte, 100)
	n, _ := reader.Read(buf)
	t.Logf("file content: %s", string(buf[:n]))
}

func TestClient_GenerateAccessToken(t *testing.T) {
	t.Skip("gitea admin token does not have user token management scope")
}
