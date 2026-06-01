// Package store persists user-managed skills under the Leros workspace.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/leros"
	"gopkg.in/yaml.v3"
)

const (
	skillFileName          = "SKILL.md"
	maxNameLength          = 64
	maxDescriptionLength   = 1024
	maxSkillContentChars   = 100_000
	maxSupportingFileBytes = 1_048_576

	ActionCreate     = "create"
	ActionPatch      = "patch"
	ActionWriteFile  = "write_file"
	ActionRemoveFile = "remove_file"
)

var (
	namePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	allowedSubdirs = []string{"assets", "references", "scripts", "templates"}
)

// SkillStore 管理文件型 Skill。
type SkillStore struct {
	rootDir string
}

// Skill 描述一个已发现的 Skill 目录。
type Skill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Result 表示 Skill 变更操作的返回结果。
type Result struct {
	Success bool   `json:"success"`
	Action  string `json:"action"`
	Name    string `json:"name"`
	Message string `json:"message,omitempty"`
	Path    string `json:"path,omitempty"`
	Error   string `json:"error,omitempty"`
}

// CreateRequest 表示创建新 Skill 目录和 SKILL.md 的请求。
type CreateRequest struct {
	Name    string
	Content string
}

// PatchRequest 表示替换 SKILL.md 或 supporting file 中文本的请求。
type PatchRequest struct {
	Name       string
	FilePath   string
	OldText    string
	NewText    string
	ReplaceAll bool
}

// WriteFileRequest 表示在 Skill 目录下写入 supporting file 的请求。
type WriteFileRequest struct {
	Name        string
	FilePath    string
	FileContent string
}

// RemoveFileRequest 表示删除 Skill 目录下 supporting file 的请求。
type RemoveFileRequest struct {
	Name     string
	FilePath string
}

// DefaultSkillRoot 返回默认 workspace skills 目录。
func DefaultSkillRoot() (string, error) {
	return leros.SkillsDir()
}

// NewSkillStore 创建以 rootDir 为根目录的 SkillStore；rootDir 为空时使用默认 Leros skills 根目录。
func NewSkillStore(rootDir string) (*SkillStore, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		var err error
		rootDir, err = DefaultSkillRoot()
		if err != nil {
			return nil, err
		}
	}
	absolute, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve skill root: %w", err)
	}
	return &SkillStore{rootDir: absolute}, nil
}

// RootDir 返回 skills 根目录。
func (s *SkillStore) RootDir() string {
	if s == nil {
		return ""
	}
	return s.rootDir
}

// Create 写入一个新 Skill。
func (s *SkillStore) Create(ctx context.Context, req CreateRequest) (*Result, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := s.validate(); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	content := strings.TrimSpace(req.Content)
	if err := validateName(name, "skill name"); err != nil {
		return nil, err
	}
	if err := validateSkillDocument(content); err != nil {
		return nil, err
	}

	if existing, err := s.Find(ctx, name); err == nil && existing != nil {
		return failure(ActionCreate, name, fmt.Sprintf("skill %q already exists at %s", name, existing.Path)), nil
	}

	skillDir := filepath.Join(s.rootDir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}

	skillPath := filepath.Join(skillDir, skillFileName)
	if err := atomicWrite(skillPath, content); err != nil {
		return nil, err
	}

	result := &Result{
		Success: true,
		Action:  ActionCreate,
		Name:    name,
		Message: fmt.Sprintf("Skill %q created.", name),
		Path:    skillDir,
	}
	return result, nil
}

// Patch 替换 SKILL.md 或 supporting file 中的文本。
func (s *SkillStore) Patch(ctx context.Context, req PatchRequest) (*Result, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := s.validate(); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	if err := validateName(name, "skill name"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.OldText) == "" {
		return nil, fmt.Errorf("old_text is required for patch")
	}
	if req.NewText == "" && req.NewText != strings.TrimSpace(req.NewText) {
		return nil, fmt.Errorf("new_text is required for patch")
	}

	skill, err := s.Find(ctx, name)
	if err != nil {
		return failure(ActionPatch, name, err.Error()), nil
	}

	targetPath := filepath.Join(skill.Path, skillFileName)
	if strings.TrimSpace(req.FilePath) != "" {
		if err := validateSupportingFilePath(req.FilePath); err != nil {
			return nil, err
		}
		targetPath, err = resolveInside(skill.Path, req.FilePath)
		if err != nil {
			return nil, err
		}
	}

	contentBytes, err := os.ReadFile(targetPath)
	if err != nil {
		return failure(ActionPatch, name, fmt.Sprintf("read target file: %v", err)), nil
	}
	content := string(contentBytes)
	count := strings.Count(content, req.OldText)
	if count == 0 {
		return failure(ActionPatch, name, "old_text was not found"), nil
	}
	if count > 1 && !req.ReplaceAll {
		return failure(ActionPatch, name, "old_text matched multiple locations; pass replace_all=true or provide a more unique old_text"), nil
	}

	newContent := strings.Replace(content, req.OldText, req.NewText, replacementCount(req.ReplaceAll))
	if strings.TrimSpace(req.FilePath) == "" {
		if err := validateSkillDocument(newContent); err != nil {
			return failure(ActionPatch, name, fmt.Sprintf("patch would break SKILL.md: %v", err)), nil
		}
	} else {
		if err := validateSupportingFileContent(req.FilePath, newContent); err != nil {
			return nil, err
		}
	}

	if err := atomicWrite(targetPath, newContent); err != nil {
		return nil, err
	}

	result := &Result{
		Success: true,
		Action:  ActionPatch,
		Name:    name,
		Message: fmt.Sprintf("Patched skill %q with %d replacement(s).", name, count),
		Path:    targetPath,
	}
	return result, nil
}

// WriteFile 在已有 Skill 下写入 supporting file。
func (s *SkillStore) WriteFile(ctx context.Context, req WriteFileRequest) (*Result, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := s.validate(); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	if err := validateName(name, "skill name"); err != nil {
		return nil, err
	}
	if err := validateSupportingFilePath(req.FilePath); err != nil {
		return nil, err
	}
	if err := validateSupportingFileContent(req.FilePath, req.FileContent); err != nil {
		return nil, err
	}

	skill, err := s.Find(ctx, name)
	if err != nil {
		return failure(ActionWriteFile, name, err.Error()), nil
	}
	targetPath, err := resolveInside(skill.Path, req.FilePath)
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(targetPath, req.FileContent); err != nil {
		return nil, err
	}

	result := &Result{
		Success: true,
		Action:  ActionWriteFile,
		Name:    name,
		Message: fmt.Sprintf("File %q written to skill %q.", req.FilePath, name),
		Path:    targetPath,
	}
	return result, nil
}

// RemoveFile 删除已有 Skill 下的 supporting file。
func (s *SkillStore) RemoveFile(ctx context.Context, req RemoveFileRequest) (*Result, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := s.validate(); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	if err := validateName(name, "skill name"); err != nil {
		return nil, err
	}
	if err := validateSupportingFilePath(req.FilePath); err != nil {
		return nil, err
	}

	skill, err := s.Find(ctx, name)
	if err != nil {
		return failure(ActionRemoveFile, name, err.Error()), nil
	}
	targetPath, err := resolveInside(skill.Path, req.FilePath)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(targetPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return failure(ActionRemoveFile, name, fmt.Sprintf("file %q not found", req.FilePath)), nil
		}
		return nil, fmt.Errorf("remove skill file: %w", err)
	}
	removeEmptyParents(skill.Path, filepath.Dir(targetPath))

	result := &Result{
		Success: true,
		Action:  ActionRemoveFile,
		Name:    name,
		Message: fmt.Sprintf("File %q removed from skill %q.", req.FilePath, name),
		Path:    targetPath,
	}
	return result, nil
}

// Find 在 skills 根目录下按目录名查找 Skill。
func (s *SkillStore) Find(ctx context.Context, name string) (*Skill, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if err := validateName(name, "skill name"); err != nil {
		return nil, err
	}

	var found *Skill
	err := filepath.WalkDir(s.rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if path == s.rootDir {
			return nil
		}
		if d.Name() != name {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, skillFileName)); err != nil {
			return nil
		}
		found = &Skill{Name: name, Path: path}
		return filepath.SkipAll
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("skill %q not found", name)
		}
		return nil, fmt.Errorf("find skill %q: %w", name, err)
	}
	if found == nil {
		return nil, fmt.Errorf("skill %q not found", name)
	}
	return found, nil
}

func (s *SkillStore) validate() error {
	if s == nil {
		return fmt.Errorf("skill store is nil")
	}
	if strings.TrimSpace(s.rootDir) == "" {
		return fmt.Errorf("skill root is required")
	}
	return nil
}

func validateName(name string, label string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s is required", label)
	}
	if len(name) > maxNameLength {
		return fmt.Errorf("%s exceeds %d characters", label, maxNameLength)
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid %s %q: use lowercase letters, numbers, hyphens, dots, and underscores; must start with a letter or digit", label, name)
	}
	return nil
}

func validateSkillDocument(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("SKILL.md content cannot be empty")
	}
	if len(content) > maxSkillContentChars {
		return fmt.Errorf("SKILL.md exceeds %d characters", maxSkillContentChars)
	}
	if !strings.HasPrefix(content, "---") {
		return fmt.Errorf("SKILL.md must start with YAML frontmatter")
	}
	parts := strings.SplitN(content[3:], "\n---", 2)
	if len(parts) != 2 {
		return fmt.Errorf("SKILL.md frontmatter is not closed")
	}
	var frontmatter struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(parts[0]), &frontmatter); err != nil {
		return fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}
	if strings.TrimSpace(frontmatter.Name) == "" {
		return fmt.Errorf("frontmatter must include name")
	}
	if strings.TrimSpace(frontmatter.Description) == "" {
		return fmt.Errorf("frontmatter must include description")
	}
	if len(frontmatter.Description) > maxDescriptionLength {
		return fmt.Errorf("description exceeds %d characters", maxDescriptionLength)
	}
	body := strings.TrimSpace(strings.TrimPrefix(parts[1], "\n"))
	if body == "" {
		return fmt.Errorf("SKILL.md must have content after frontmatter")
	}
	return nil
}

func validateSupportingFilePath(filePath string) error {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("file_path is required")
	}
	if filepath.IsAbs(filePath) {
		return fmt.Errorf("absolute file_path is not allowed")
	}
	clean := filepath.Clean(filePath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path traversal is not allowed")
	}
	parts := strings.Split(filepath.ToSlash(clean), "/")
	if len(parts) < 2 {
		return fmt.Errorf("file_path must include a file under %s", strings.Join(allowedSubdirs, ", "))
	}
	if !slices.Contains(allowedSubdirs, parts[0]) {
		return fmt.Errorf("file_path must be under one of: %s", strings.Join(allowedSubdirs, ", "))
	}
	return nil
}

func validateSupportingFileContent(filePath string, content string) error {
	if len(content) > maxSkillContentChars {
		return fmt.Errorf("%s exceeds %d characters", filePath, maxSkillContentChars)
	}
	if len([]byte(content)) > maxSupportingFileBytes {
		return fmt.Errorf("%s exceeds %d bytes", filePath, maxSupportingFileBytes)
	}
	return nil
}

func resolveInside(root string, relativePath string) (string, error) {
	target := filepath.Join(root, filepath.Clean(relativePath))
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("target path escapes skill directory")
	}
	return target, nil
}

func atomicWrite(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}

func replacementCount(replaceAll bool) int {
	if replaceAll {
		return -1
	}
	return 1
}

func removeEmptyParents(root string, current string) {
	for current != root && strings.HasPrefix(current, root) {
		entries, err := os.ReadDir(current)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
		current = filepath.Dir(current)
	}
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func failure(action string, name string, message string) *Result {
	return &Result{
		Success: false,
		Action:  action,
		Name:    name,
		Error:   message,
	}
}
