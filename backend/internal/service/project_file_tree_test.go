package service

import (
	"testing"

	"code.gitea.io/sdk/gitea"
	"github.com/insmtx/Leros/backend/internal/api/contract"
)

func TestIsPathAllowed(t *testing.T) {
	tests := []struct {
		path    string
		allowed bool
	}{
		{"uploads/readme.md", true},
		{"uploads/sub/dir/file.txt", true},
		{"artifacts/report.pdf", true},
		{"artifacts/", true},
		{"src/main.go", false},
		{"", false},
		{"uploads", false},
		{"artifacts", false},
		{"config.yaml", false},
		{"uploads_evil/file.txt", false},
	}

	for _, tt := range tests {
		result := isPathAllowed(tt.path)
		if result != tt.allowed {
			t.Errorf("isPathAllowed(%q) = %v, want %v", tt.path, result, tt.allowed)
		}
	}
}

func TestBuildFileTree(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads", Type: "tree"},
		{Path: "uploads/readme.md", Type: "blob", Size: 100},
		{Path: "uploads/images", Type: "tree"},
		{Path: "uploads/images/logo.png", Type: "blob", Size: 2048},
		{Path: "artifacts", Type: "tree"},
		{Path: "artifacts/report.pdf", Type: "blob", Size: 4096},
	}

	roots := buildFileTree(entries, nil)

	if len(roots) != 2 {
		t.Fatalf("expected 2 roots, got %d", len(roots))
	}

	// root: uploads
	uploads := roots[0]
	if uploads.Name != "uploads" || uploads.Type != "directory" {
		t.Errorf("root[0] expected uploads directory, got %+v", uploads)
	}
	if len(uploads.Children) != 2 {
		t.Errorf("uploads expected 2 children, got %d", len(uploads.Children))
	}

	// uploads/readme.md
	if uploads.Children[0].Name != "readme.md" || uploads.Children[0].Type != "file" {
		t.Errorf("uploads child[0] expected readme.md file, got %+v", uploads.Children[0])
	}

	// uploads/images
	images := uploads.Children[1]
	if images.Name != "images" || images.Type != "directory" {
		t.Errorf("uploads child[1] expected images directory, got %+v", images)
	}
	if len(images.Children) != 1 {
		t.Errorf("images expected 1 child, got %d", len(images.Children))
	}

	// uploads/images/logo.png
	if images.Children[0].Name != "logo.png" || images.Children[0].Type != "file" || images.Children[0].Size != 2048 {
		t.Errorf("images child expected logo.png file size=2048, got %+v", images.Children[0])
	}

	// root: artifacts
	artifacts := roots[1]
	if artifacts.Name != "artifacts" || artifacts.Type != "directory" {
		t.Errorf("root[1] expected artifacts directory, got %+v", artifacts)
	}
	if len(artifacts.Children) != 1 {
		t.Errorf("artifacts expected 1 child, got %d", len(artifacts.Children))
	}
	if artifacts.Children[0].Name != "report.pdf" || artifacts.Children[0].Type != "file" || artifacts.Children[0].Size != 4096 {
		t.Errorf("artifacts child expected report.pdf file size=4096, got %+v", artifacts.Children[0])
	}
}

func TestBuildFileTree_Empty(t *testing.T) {
	roots := buildFileTree(nil, nil)
	if len(roots) != 0 {
		t.Errorf("expected empty roots, got %v", roots)
	}
}

func TestBuildFileTree_DeepNested(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/a/b/c", Type: "tree"},
		{Path: "uploads/a/b/c/deep.txt", Type: "blob", Size: 42},
	}

	roots := buildFileTree(entries, nil)

	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	a := roots[0]
	if a.Name != "uploads" || a.Type != "directory" {
		t.Errorf("root expected uploads, got %+v", a)
	}
	if len(a.Children) != 1 {
		t.Fatalf("uploads expected 1 child, got %d", len(a.Children))
	}
	b := a.Children[0]
	if b.Name != "a" || b.Type != "directory" {
		t.Errorf("b expected a directory, got %+v", b)
	}
	if len(b.Children) != 1 {
		t.Fatalf("a expected 1 child, got %d", len(b.Children))
	}
	c := b.Children[0]
	if c.Name != "b" || c.Type != "directory" {
		t.Errorf("c expected b directory, got %+v", c)
	}
	if len(c.Children) != 1 {
		t.Fatalf("b expected 1 child, got %d", len(c.Children))
	}
	d := c.Children[0]
	if d.Name != "c" || d.Type != "directory" {
		t.Errorf("d expected c directory, got %+v", d)
	}
	if len(d.Children) != 1 {
		t.Fatalf("c expected 1 child, got %d", len(d.Children))
	}
	if d.Children[0].Name != "deep.txt" || d.Children[0].Type != "file" {
		t.Errorf("deep expected deep.txt file, got %+v", d.Children[0])
	}
}

func TestFilterByParentPaths_Root(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/readme.md", Type: "blob", Size: 100},
	}
	roots := buildFileTree(entries, nil)
	result := filterByParentPaths(roots, "")
	if len(result) != 1 || result[0].Name != "uploads" {
		t.Errorf("expected 1 root uploads, got %v", result)
	}
}

func TestFilterByParentPaths_SubDir(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/images/logo.png", Type: "blob", Size: 2048},
	}
	roots := buildFileTree(entries, nil)
	result := filterByParentPaths(roots, "uploads/images")
	if len(result) != 1 || result[0].Name != "images" || result[0].Type != "directory" {
		t.Errorf("expected 1 directory images, got %v", result)
	}
}

func TestFilterByParentPaths_NotFound(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/readme.md", Type: "blob", Size: 100},
	}
	roots := buildFileTree(entries, nil)
	result := filterByParentPaths(roots, "nonexistent")
	if result != nil {
		t.Errorf("expected nil for nonexistent path, got %v", result)
	}
}

func TestFilterByParentPaths_HasLeadingSlash(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/readme.md", Type: "blob", Size: 100},
	}
	roots := buildFileTree(entries, nil)
	result := filterByParentPaths(roots, "/uploads/")
	if len(result) != 1 || result[0].Name != "uploads" || result[0].Type != "directory" {
		t.Errorf("expected 1 directory uploads, got %v", result)
	}
}

func TestFilterByParentPaths_RootSlash(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/readme.md", Type: "blob", Size: 100},
	}
	roots := buildFileTree(entries, nil)
	result := filterByParentPaths(roots, "/")
	if len(result) != 1 || result[0].Name != "uploads" {
		t.Errorf("expected 1 root uploads for /, got %v", result)
	}
}

func TestFilterByParentPaths_FilePath(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/readme.md", Type: "blob", Size: 100},
		{Path: "uploads/images/logo.png", Type: "blob", Size: 2048},
	}
	roots := buildFileTree(entries, nil)
	result := filterByParentPaths(roots, "uploads/readme.md")
	if len(result) != 1 || result[0].Name != "readme.md" || result[0].Type != "file" {
		t.Errorf("expected 1 file readme.md, got %v", result)
	}
}

func TestMimeTypeByExt(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"image.png", "image/png"},
		{"data.json", "application/json"},
	}

	for _, tt := range tests {
		got := mimeTypeByExt(tt.filename)
		if got != tt.want {
			t.Errorf("mimeTypeByExt(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}

	if got := mimeTypeByExt("script.js"); got == "" {
		t.Errorf("mimeTypeByExt(\"script.js\") should return non-empty mime type")
	}

	if got := mimeTypeByExt("noext"); got != "" {
		t.Errorf("mimeTypeByExt(\"noext\") = %q, want \"\"", got)
	}
}

// compile-time check: mockProjectServiceForAddFile implements contract.ProjectService
type _assertMockImplementsProjectService = contract.ProjectService

var _ _assertMockImplementsProjectService = (*mockProjectServiceForAddFile)(nil)

func TestBuildFileTreeWithCreatedAt(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/a.txt", Type: "blob", Size: 100},
		{Path: "uploads/b.txt", Type: "blob", Size: 200},
		{Path: "uploads/c.txt", Type: "blob", Size: 300},
	}

	createdAtMap := map[string]int64{
		"uploads/a.txt": 1700000000,
		"uploads/c.txt": 1700000100,
	}

	roots := buildFileTree(entries, createdAtMap)

	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	uploads := roots[0]
	if len(uploads.Children) != 3 {
		t.Fatalf("expected 3 children, got %d", len(uploads.Children))
	}

	a := uploads.Children[0]
	if a.Name != "a.txt" || a.CreatedAt != 1700000000 {
		t.Errorf("a.txt expected CreatedAt=1700000000, got %d", a.CreatedAt)
	}

	b := uploads.Children[1]
	if b.Name != "b.txt" || b.CreatedAt != 0 {
		t.Errorf("b.txt expected CreatedAt=0 (not in map), got %d", b.CreatedAt)
	}

	c := uploads.Children[2]
	if c.Name != "c.txt" || c.CreatedAt != 1700000100 {
		t.Errorf("c.txt expected CreatedAt=1700000100, got %d", c.CreatedAt)
	}
}

func TestBuildFileTreeWithCreatedAtForDirectory(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads", Type: "tree"},
		{Path: "uploads/a.txt", Type: "blob", Size: 100},
	}

	createdAtMap := map[string]int64{
		"uploads/a.txt": 1700000000,
	}

	roots := buildFileTree(entries, createdAtMap)

	uploads := roots[0]
	if uploads.Type != "directory" {
		t.Fatalf("expected directory, got %s", uploads.Type)
	}
	if uploads.CreatedAt != 0 {
		t.Errorf("directory node expected CreatedAt=0, got %d", uploads.CreatedAt)
	}
}

func TestBuildFileTreeWithNilMap(t *testing.T) {
	entries := []gitea.GitEntry{
		{Path: "uploads/a.txt", Type: "blob", Size: 100},
	}

	roots := buildFileTree(entries, nil)

	uploads := roots[0]
	if len(uploads.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(uploads.Children))
	}
	if uploads.Children[0].CreatedAt != 0 {
		t.Errorf("nil map: expected CreatedAt=0, got %d", uploads.Children[0].CreatedAt)
	}
}

func TestLookupFileCreatedAt_EmptyPaths(t *testing.T) {
	// 不需要真实 gitea client — empty paths 直接返回空 map
	result := (&projectService{}).lookupFileCreatedAt(nil, "", "", "", nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %+v", result)
	}
}
