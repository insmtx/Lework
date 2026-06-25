package fetch

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGitHubSourceFetchVersionBranchSuccess(t *testing.T) {
	zipBytes := testSkillZip(t, "repo-main/skills/demo/SKILL.md", testSkillContent("demo"))
	requests := make([]string, 0, 1)
	source := &GitHubSource{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.String())
				return response(http.StatusOK, zipBytes), nil
			}),
		},
	}

	bundle, err := source.FetchVersion(context.Background(), "owner/repo/skills/demo", "main")
	if err != nil {
		t.Fatalf("FetchVersion returned error: %v", err)
	}
	defer func() {
		if bundle.TempDir != "" {
			_ = os.RemoveAll(bundle.TempDir)
		}
	}()
	if string(bundle.Content) != testSkillContent("demo") {
		t.Fatalf("content = %q, want skill content", string(bundle.Content))
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if !strings.Contains(requests[0], "/archive/refs/heads/main.zip") {
		t.Fatalf("request URL = %q, want branch zip URL", requests[0])
	}
}

func TestGitHubSourceFetchVersionRootSkill(t *testing.T) {
	zipBytes := testSkillZipEntries(t, map[string]string{
		"repo-main/SKILL.md":                     testSkillContent("video-use"),
		"repo-main/scripts/edit.mjs":             "console.log('edit')\n",
		"repo-main/references/style-guide.md":    "# Style\n",
		"repo-main/helpers/internal.py":          "print('ignored')\n",
		"repo-main/skills/other/SKILL.md":        testSkillContent("other"),
		"repo-main/skills/other/scripts/run.mjs": "console.log('ignored')\n",
	})
	source := &GitHubSource{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return response(http.StatusOK, zipBytes), nil
			}),
		},
	}

	bundle, err := source.FetchVersion(context.Background(), "owner/repo/.", "main")
	if err != nil {
		t.Fatalf("FetchVersion returned error: %v", err)
	}
	defer func() {
		if bundle.TempDir != "" {
			_ = os.RemoveAll(bundle.TempDir)
		}
	}()
	if string(bundle.Content) != testSkillContent("video-use") {
		t.Fatalf("content = %q, want root skill content", string(bundle.Content))
	}
	if bundle.Meta.Identifier != "owner/repo/." {
		t.Fatalf("identifier = %q, want owner/repo/.", bundle.Meta.Identifier)
	}
	if string(bundle.Files["scripts/edit.mjs"]) != "console.log('edit')\n" {
		t.Fatalf("scripts/edit.mjs was not collected")
	}
	if string(bundle.Files["references/style-guide.md"]) != "# Style\n" {
		t.Fatalf("references/style-guide.md was not collected")
	}
	if _, ok := bundle.Files["helpers/internal.py"]; ok {
		t.Fatalf("helpers/internal.py should not be collected")
	}
	if _, ok := bundle.Files["skills/other/scripts/run.mjs"]; ok {
		t.Fatalf("nested skill files should not be collected for root skill")
	}
}

func TestGitHubSourceFetchRepositoryRootFallbackToSingleNestedSkill(t *testing.T) {
	zipBytes := testSkillZipEntries(t, map[string]string{
		"repo-main/README.md":                     "# Demo\n",
		"repo-main/skills/demo/SKILL.md":          testSkillContent("demo"),
		"repo-main/skills/demo/scripts/run.mjs":   "console.log('run')\n",
		"repo-main/skills/demo/references/doc.md": "# Doc\n",
	})
	source := &GitHubSource{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return response(http.StatusOK, zipBytes), nil
			}),
		},
	}

	bundle, err := source.Fetch(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	defer func() {
		if bundle.TempDir != "" {
			_ = os.RemoveAll(bundle.TempDir)
		}
	}()
	if string(bundle.Content) != testSkillContent("demo") {
		t.Fatalf("content = %q, want nested skill content", string(bundle.Content))
	}
	if bundle.Meta.Identifier != "owner/repo/skills/demo" {
		t.Fatalf("identifier = %q, want owner/repo/skills/demo", bundle.Meta.Identifier)
	}
	if string(bundle.Files["scripts/run.mjs"]) != "console.log('run')\n" {
		t.Fatalf("scripts/run.mjs was not collected")
	}
}

func TestGitHubSourceFetchRepositoryRootMultipleNestedSkills(t *testing.T) {
	zipBytes := testSkillZipEntries(t, map[string]string{
		"repo-main/skills/one/SKILL.md": testSkillContent("one"),
		"repo-main/skills/two/SKILL.md": testSkillContent("two"),
	})
	source := &GitHubSource{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return response(http.StatusOK, zipBytes), nil
			}),
		},
	}

	_, err := source.FetchVersion(context.Background(), "owner/repo/.", "main")
	if err == nil {
		t.Fatal("expected multiple SKILL.md error")
	}
	if !strings.Contains(err.Error(), "multiple SKILL.md") {
		t.Fatalf("error = %q, want multiple SKILL.md", err.Error())
	}
}

func TestGitHubSourceFetchVersionTagFallback(t *testing.T) {
	zipBytes := testSkillZip(t, "repo-v1.0.0/skills/demo/SKILL.md", testSkillContent("demo"))
	requests := make([]string, 0, 2)
	source := &GitHubSource{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.String())
				if strings.Contains(req.URL.Path, "/archive/refs/heads/") {
					return response(http.StatusNotFound, nil), nil
				}
				return response(http.StatusOK, zipBytes), nil
			}),
		},
	}

	bundle, err := source.FetchVersion(context.Background(), "owner/repo/skills/demo", "v1.0.0")
	if err != nil {
		t.Fatalf("FetchVersion returned error: %v", err)
	}
	defer func() {
		if bundle.TempDir != "" {
			_ = os.RemoveAll(bundle.TempDir)
		}
	}()
	if string(bundle.Content) != testSkillContent("demo") {
		t.Fatalf("content = %q, want skill content", string(bundle.Content))
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if !strings.Contains(requests[0], "/archive/refs/heads/v1.0.0.zip") {
		t.Fatalf("first request URL = %q, want branch zip URL", requests[0])
	}
	if !strings.Contains(requests[1], "/archive/refs/tags/v1.0.0.zip") {
		t.Fatalf("second request URL = %q, want tag zip URL", requests[1])
	}
}

func TestGitHubSourceFetchVersionFailure(t *testing.T) {
	source := &GitHubSource{
		client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return response(http.StatusNotFound, nil), nil
			}),
		},
	}

	_, err := source.FetchVersion(context.Background(), "owner/repo/skills/demo", "missing")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "branch") || !strings.Contains(err.Error(), "tag") {
		t.Fatalf("error = %q, want branch and tag details", err.Error())
	}
}

func testSkillZip(t *testing.T, filePath string, content string) []byte {
	t.Helper()
	return testSkillZipEntries(t, map[string]string{filePath: content})
}

func testSkillZipEntries(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for filePath, content := range entries {
		w, err := zw.Create(filePath)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func testSkillContent(name string) string {
	return "---\nname: " + name + "\ndescription: test skill\n---\n\nUse this skill for tests.\n"
}

func response(status int, body []byte) *http.Response {
	if body == nil {
		body = []byte{}
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}
