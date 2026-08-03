package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestPluginIdentityFromSkillArchiveUsesManifestName(t *testing.T) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create("demo/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("---\nname: release-notes\ndescription: Release notes helper\n---\n\nWrite release notes.")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	code, name, description, err := pluginIdentityFromSkillArchive(buf.Bytes())
	if err != nil || code != "release-notes" || name != "release-notes" || description != "Release notes helper" {
		t.Fatalf("identity=%q,%q,%q err=%v", code, name, description, err)
	}
}

func TestNormalizeSkillArchiveStripsSkillParentDirectory(t *testing.T) {
	archive := testSkillArchive(t, map[string]string{
		"bundle/SKILL.md":          "---\nname: release-notes\ndescription: Release notes helper\n---\n\nWrite release notes.",
		"bundle/references/api.md": "API reference",
		"outside/ignored.md":       "must not be packaged",
	})
	normalized, changed, err := normalizeSkillArchive(archive)
	if err != nil {
		t.Fatalf("normalize skill archive: %v", err)
	}
	if !changed {
		t.Fatal("nested skill archive must be normalized")
	}
	reader, err := zip.NewReader(bytes.NewReader(normalized), int64(len(normalized)))
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string]bool)
	for _, file := range reader.File {
		entries[file.Name] = true
	}
	if !entries["SKILL.md"] || !entries["references/api.md"] || entries["bundle/SKILL.md"] || entries["outside/ignored.md"] {
		t.Fatalf("normalized archive entries = %#v", entries)
	}
}

func TestNormalizeSkillArchiveRetainsRootDirectoryPackage(t *testing.T) {
	archive := testSkillArchive(t, map[string]string{
		"SKILL.md":          "---\nname: release-notes\ndescription: Release notes helper\n---\n\nWrite release notes.",
		"references/api.md": "API reference",
	})
	normalized, changed, err := normalizeSkillArchive(archive)
	if err != nil {
		t.Fatalf("normalize root skill archive: %v", err)
	}
	if changed || !bytes.Equal(normalized, archive) {
		t.Fatal("root skill archive must be retained without repackaging")
	}
}

func TestBuildSkillRevisionContentStoresRawEntrypointAndSortedFiles(t *testing.T) {
	rawSkillMD := "---\nname: release-notes\ndescription: Release notes helper\n---\n\n# Release notes"
	archive := testSkillArchive(t, map[string]string{
		"scripts/z.sh":    "z",
		"SKILL.md":        rawSkillMD,
		"references/a.md": "a",
		"scripts/a.sh":    "a",
	})
	hash := sha256.Sum256(archive)
	draft, err := buildSkillRevisionContent(archive, hex.EncodeToString(hash[:]))
	if err != nil {
		t.Fatalf("build Skill revision content: %v", err)
	}
	if draft.EntrypointContent != rawSkillMD {
		t.Fatalf("entrypoint content = %q", draft.EntrypointContent)
	}
	gotPaths := make([]string, 0, len(draft.FileIndex))
	for _, file := range draft.FileIndex {
		gotPaths = append(gotPaths, file.Path)
		if len(file.SHA256) != 64 {
			t.Fatalf("file %s SHA-256 = %q", file.Path, file.SHA256)
		}
	}
	wantPaths := []string{"SKILL.md", "references/a.md", "scripts/a.sh", "scripts/z.sh"}
	if len(gotPaths) != len(wantPaths) {
		t.Fatalf("file paths = %#v", gotPaths)
	}
	for index := range wantPaths {
		if gotPaths[index] != wantPaths[index] {
			t.Fatalf("file paths = %#v, want %#v", gotPaths, wantPaths)
		}
	}
}

func TestValidatePluginDefinition(t *testing.T) {
	cases := []struct {
		kind       string
		definition string
		ok         bool
	}{
		{"skill", `{"schema":"skill/v1","artifact":{"file_upload_id":"file_demo","sha256":"abc","size_bytes":1,"content_type":"application/zip"}}`, true},
		{"mcp", `{"schema":"mcp/v1","transport":"http","url":"https://mcp.example.com","secret_refs":{"authorization":"sec_1"}}`, true},
		{"mcp", `{"schema":"mcp/v1","transport":"stdio","command":"mcp-server","args":[],"env_secret_refs":{"TOKEN":"sec_1"}}`, true},
		{"workflow", `{"schema":"workflow/v1","definition":{"steps":[]}}`, true},
		{"mcp", `{"schema":"mcp/v1","transport":"http","url":"https://mcp.example.com","token":"plaintext"}`, true},
		{"unknown", `{"schema":"unknown/v1"}`, false},
	}
	for _, tc := range cases {
		err := ValidatePluginDefinition(tc.kind, json.RawMessage(tc.definition))
		if (err == nil) != tc.ok {
			t.Errorf("ValidatePluginDefinition(%s) error=%v, want ok=%v", tc.kind, err, tc.ok)
		}
	}
}

func TestArtifactFromDefinition(t *testing.T) {
	artifact, err := ArtifactFromDefinition("skill", json.RawMessage(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_demo","sha256":"abc"}}`))
	if err != nil || artifact == nil || artifact.FileUploadID != "file_demo" || artifact.SHA256 != "abc" {
		t.Fatalf("artifact = %#v, %v", artifact, err)
	}
}

func testSkillArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
