//go:build integration

package gitea_test

import (
	"encoding/base64"
	"testing"
	"time"

	"code.gitea.io/sdk/gitea"
)

const (
	testEndpoint = "xxx"
	testToken    = "xxx"
	testOwner    = "xxx"
)

func newTestClient(t *testing.T) *gitea.Client {
	t.Helper()
	client, err := gitea.NewClient(testEndpoint, gitea.SetToken(testToken))
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return client
}

func TestClient_CreateAndDeleteRepo(t *testing.T) {
	client := newTestClient(t)

	repoName := "test-create-" + time.Now().Format("200601021504")
	repo, _, err := client.AdminCreateRepo(testOwner, gitea.CreateRepoOption{
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

	_, err = client.DeleteRepo(testOwner, repoName)
	if err != nil {
		t.Errorf("delete repo: %v", err)
	}
}

func TestClient_GetRepo(t *testing.T) {
	client := newTestClient(t)

	_, _, err := client.GetRepo(testOwner, "test-get-"+time.Now().Format("1504"))
	if err != nil {
		t.Logf("repo not found (expected for non-existent repo): %v", err)
	}
}

func TestClient_ListContents(t *testing.T) {
	client := newTestClient(t)

	repoName := "test-contents-" + time.Now().Format("200601021504")
	_, _, err := client.AdminCreateRepo(testOwner, gitea.CreateRepoOption{
		Name:        repoName,
		Description: "test contents",
		Private:     true,
		AutoInit:    true,
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	defer func() {
		_, _ = client.DeleteRepo(testOwner, repoName)
	}()

	entries, _, err := client.ListContents(testOwner, repoName, "main", "")
	if err != nil {
		t.Fatalf("list contents: %v", err)
	}
	t.Logf("root entries: %d", len(entries))
	for _, e := range entries {
		t.Logf("  %s (type=%s, size=%d)", e.Name, e.Type, e.Size)
	}
}

func TestClient_CreateFileAndGetFile(t *testing.T) {
	client := newTestClient(t)

	repoName := "test-file-" + time.Now().Format("200601021504")
	_, _, err := client.AdminCreateRepo(testOwner, gitea.CreateRepoOption{
		Name:        repoName,
		Description: "test file read",
		Private:     true,
		AutoInit:    true,
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	defer func() {
		_, _ = client.DeleteRepo(testOwner, repoName)
	}()

	_, _, err = client.CreateFile(testOwner, repoName, "hello.md", gitea.CreateFileOptions{
		FileOptions: gitea.FileOptions{
			Message: "add hello.md",
		},
		Content: "SGVsbG8gV29ybGQ=",
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	data, _, err := client.GetFile(testOwner, repoName, "main", "hello.md")
	if err != nil {
		t.Fatalf("get file: %v", err)
	}
	t.Logf("file content: %s", string(data))
}

func TestClient_GetRepoTree(t *testing.T) {
	client := newTestClient(t)

	repoName := "test-tree-" + time.Now().Format("200601021504")
	_, _, err := client.AdminCreateRepo(testOwner, gitea.CreateRepoOption{
		Name:        repoName,
		Description: "test tree",
		Private:     true,
		AutoInit:    true,
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	defer func() {
		_, _ = client.DeleteRepo(testOwner, repoName)
	}()

	_, _, err = client.CreateFile(testOwner, repoName, "readme.md", gitea.CreateFileOptions{
		FileOptions: gitea.FileOptions{
			Message: "init readme",
		},
		Content: base64.StdEncoding.EncodeToString([]byte("# " + repoName)),
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	treeResp, _, err := client.GetTrees(testOwner, repoName, gitea.ListTreeOptions{
		Ref:       "main",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("get tree: %v", err)
	}
	t.Logf("tree entries: %d", len(treeResp.Entries))
	for _, e := range treeResp.Entries {
		t.Logf("  %s (type=%s, size=%d)", e.Path, e.Type, e.Size)
	}
}

func TestClient_GenerateAccessToken(t *testing.T) {
	t.Skip("gitea admin token does not have user token management scope")
}
