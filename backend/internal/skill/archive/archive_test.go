package skillarchive

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"testing"
)

func TestValidateRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []zipEntry
	}{
		{name: "traversal", entries: []zipEntry{{name: "../SKILL.md", body: validSkill}}},
		{name: "duplicate manifest", entries: []zipEntry{{name: "SKILL.md", body: validSkill}, {name: "nested/SKILL.md", body: validSkill}}},
		{name: "duplicate normalized path", entries: []zipEntry{{name: "SKILL.md", body: validSkill}, {name: "a/../file.txt", body: "one"}, {name: "file.txt", body: "two"}}},
		{name: "symlink", entries: []zipEntry{{name: "SKILL.md", body: validSkill}, {name: "link", body: "target", mode: fs.ModeSymlink | 0o777}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(makeZip(t, tc.entries...)); err == nil {
				t.Fatal("Validate() unexpectedly succeeded")
			}
		})
	}
}

func TestExtractCreatesOnlyRegularSkillFiles(t *testing.T) {
	dest := t.TempDir()
	archive := makeZip(t, zipEntry{name: "SKILL.md", body: validSkill}, zipEntry{name: "references/a.md", body: "reference"})
	if err := Extract(archive, dest); err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
}

const validSkill = "---\nname: test\ndescription: test\n---\nUse this skill.\n"

type zipEntry struct {
	name string
	body string
	mode fs.FileMode
}

func makeZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
