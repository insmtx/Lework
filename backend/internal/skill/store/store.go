// Package store persists user-managed skills under the Leros workspace.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/ygpkg/yg-go/logs"
)

const (
	skillFileName          = "SKILL.md"
	seedManifestFile       = ".seed-manifest"
	maxNameLength          = 64
	maxDescriptionLength   = 1024
	maxSkillContentChars   = 1_048_576
	maxSupportingFileBytes = 1_048_576

	ActionCreate     = "create"
	ActionPatch      = "patch"
	ActionEdit       = "edit"
	ActionDelete     = "delete"
	ActionWriteFile  = "write_file"
	ActionRemoveFile = "remove_file"
)

var (
	namePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	allowedSubdirs = []string{"assets", "references", "scripts", "templates"}
)

// MutationKind 表示一次 skill 变更的类型。
type MutationKind int

const (
	MutationCreate MutationKind = iota
	MutationModify
	MutationDelete
)

// SkillStore 管理文件型 Skill。
type SkillStore struct {
	rootDir    string
	OnMutation func(ctx context.Context, kind MutationKind, name, action string)
}

// mutationKindForAction 将 action 字符串映射为 MutationKind。
func mutationKindForAction(action string) MutationKind {
	switch action {
	case ActionCreate:
		return MutationCreate
	case ActionDelete:
		return MutationDelete
	default:
		return MutationModify
	}
}

// Skill 描述一个已发现的 Skill 目录。
type Skill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// SkillError 表示可被调用方程序化匹配的业务错误。
type SkillError struct {
	Code    string
	Message string
}

func (e *SkillError) Error() string {
	return e.Message
}

// 预定义 sentinel error，调用方可通过 errors.Is 匹配。
var (
	ErrSkillExists     = &SkillError{Code: "skill_exists", Message: "skill already exists"}
	ErrSkillNotFound   = &SkillError{Code: "skill_not_found", Message: "skill not found"}
	ErrSkillIsBuiltin  = &SkillError{Code: "skill_is_builtin", Message: "cannot uninstall built-in skill"}
	ErrNameInvalid     = &SkillError{Code: "name_invalid", Message: "invalid skill name"}
	ErrDocumentInvalid = &SkillError{Code: "document_invalid", Message: "invalid skill document"}
	ErrPatchNoMatch    = &SkillError{Code: "patch_no_match", Message: "old_text was not found"}
	ErrPatchAmbiguous  = &SkillError{Code: "patch_ambiguous", Message: "old_text matched multiple locations"}
	ErrPathInvalid     = &SkillError{Code: "path_invalid", Message: "invalid file path"}
)

// Result 表示 Skill 变更操作的返回结果。
type Result struct {
	Success   bool   `json:"success"`
	Action    string `json:"action"`
	Name      string `json:"name"`
	Message   string `json:"message,omitempty"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// CreateRequest 表示创建新 Skill 目录和 SKILL.md 的请求。
type CreateRequest struct {
	Name    string
	Content string
}

// InstallRequest 表示安装一个完整 Skill 的请求（SKILL.md + 附属文件）。
// 先在临时目录中组装，再整体移动到最终位置。
type InstallRequest struct {
	Name    string
	Content string            // SKILL.md 内容
	Files   map[string]string // 附属文件：相对路径 → 内容
	Force   bool              // 是否覆盖已有 skill
	Source  string            // 来源（Leros / ClawHub / github / SkillsSh / url）
	SkillID string            // 源内唯一标识
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

// EditRequest 表示完整替换已有 Skill 的 SKILL.md 内容的请求。
type EditRequest struct {
	Name    string
	Content string
}

// DeleteRequest 表示删除整个 Skill 目录的请求。
type DeleteRequest struct {
	Name string
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

// notifyMutation 在变更成功后调用 OnMutation 回调（如果已设置）。
func (s *SkillStore) notifyMutation(ctx context.Context, name, action string) {
	if s.OnMutation != nil {
		s.OnMutation(ctx, mutationKindForAction(action), name, action)
	}
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
		return failure(ActionCreate, name, fmt.Sprintf("skill %q already exists at %s", name, existing.Path), ErrSkillExists), nil
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
	s.notifyMutation(ctx, name, ActionCreate)
	return result, nil
}

// Install 将完整 Skill（SKILL.md + 附属文件）先写入临时目录，验证后整体移动到最终位置。
func (s *SkillStore) Install(ctx context.Context, req InstallRequest) (*Result, error) {
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

	// 预先校验所有附属文件路径和内容。
	for relPath, fileContent := range req.Files {
		if err := validateSupportingFilePath(relPath); err != nil {
			return nil, fmt.Errorf("file %q: %w", relPath, err)
		}
		if err := validateSupportingFileContent(relPath, fileContent); err != nil {
			return nil, err
		}
	}

	skillDir := filepath.Join(s.rootDir, name)

	// 检查目标是否已存在。
	if existing, err := s.Find(ctx, name); err == nil && existing != nil {
		if !req.Force {
			return failure(ActionCreate, name, fmt.Sprintf("skill %q already exists at %s", name, existing.Path), ErrSkillExists), nil
		}
	}

	// 在 skill root 下创建临时目录，确保与目标在同一文件系统（rename 原子操作）。
	if err := os.MkdirAll(s.rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure skill root: %w", err)
	}
	tmpDir, err := os.MkdirTemp(s.rootDir, ".leros-install-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 写入 SKILL.md 到临时目录。
	if err := atomicWrite(filepath.Join(tmpDir, skillFileName), content); err != nil {
		return nil, fmt.Errorf("stage SKILL.md: %w", err)
	}

	// 写入附属文件到临时目录。
	for relPath, fileContent := range req.Files {
		destPath, err := resolveInside(tmpDir, relPath)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", relPath, err)
		}
		if err := atomicWrite(destPath, fileContent); err != nil {
			return nil, fmt.Errorf("stage %q: %w", relPath, err)
		}
	}

	// 强制覆盖时，先将旧目录 rename 到备份，确保 Rename 失败时可恢复。
	var backupPath string
	if req.Force {
		backupPath = skillDir + ".backup"
		_ = os.RemoveAll(backupPath)
		if err := os.Rename(skillDir, backupPath); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("backup existing skill: %w", err)
			}
			backupPath = ""
		}
	}

	// 整体移动到最终位置。
	if err := os.Rename(tmpDir, skillDir); err != nil {
		// 恢复旧目录
		if backupPath != "" {
			if restoreErr := os.Rename(backupPath, skillDir); restoreErr != nil {
				// 恢复失败，记录日志
				_ = restoreErr
			}
		}
		return nil, fmt.Errorf("move skill to final location: %w", err)
	}

	// 清理备份
	if backupPath != "" {
		_ = os.RemoveAll(backupPath)
	}

	// 写入 .skill-metadata 文件
	if req.Source != "" || req.SkillID != "" {
		if err := writeSkillMetadata(skillDir, req.Source, req.SkillID); err != nil {
			logs.Warnf("write .skill-metadata for %s: %v", name, err)
		}
	}

	result := &Result{
		Success: true,
		Action:  ActionCreate,
		Name:    name,
		Message: fmt.Sprintf("Skill %q installed.", name),
		Path:    skillDir,
	}
	s.notifyMutation(ctx, name, ActionCreate)
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
	// new_text 允许为空（表示删除匹配文本）；通过 tool 层 Validate 保证显式传入。

	skill, err := s.Find(ctx, name)
	if err != nil {
		return failure(ActionPatch, name, err.Error(), ErrSkillNotFound), nil
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
		return nil, fmt.Errorf("read target file %s: %w", targetPath, err)
	}
	content := string(contentBytes)
	count := strings.Count(content, req.OldText)
	if count == 0 {
		return failure(ActionPatch, name, "old_text was not found", ErrPatchNoMatch), nil
	}
	if count > 1 && !req.ReplaceAll {
		return failure(ActionPatch, name, "old_text matched multiple locations; pass replace_all=true or provide a more unique old_text", ErrPatchAmbiguous), nil
	}

	newContent := strings.Replace(content, req.OldText, req.NewText, replacementCount(req.ReplaceAll))
	if strings.TrimSpace(req.FilePath) == "" {
		if err := validateSkillDocument(newContent); err != nil {
			return failure(ActionPatch, name, fmt.Sprintf("patch would break SKILL.md: %v", err), ErrDocumentInvalid), nil
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
	s.notifyMutation(ctx, name, ActionPatch)
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
		return failure(ActionWriteFile, name, err.Error(), ErrSkillNotFound), nil
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
	s.notifyMutation(ctx, name, ActionWriteFile)
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
		return failure(ActionRemoveFile, name, err.Error(), ErrSkillNotFound), nil
	}
	targetPath, err := resolveInside(skill.Path, req.FilePath)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(targetPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return failure(ActionRemoveFile, name, fmt.Sprintf("file %q not found", req.FilePath), ErrPathInvalid), nil
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
	s.notifyMutation(ctx, name, ActionRemoveFile)
	return result, nil
}

// Edit 完全替换已有 Skill 的 SKILL.md 内容。
func (s *SkillStore) Edit(ctx context.Context, req EditRequest) (*Result, error) {
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

	skill, err := s.Find(ctx, name)
	if err != nil {
		return failure(ActionEdit, name, err.Error(), ErrSkillNotFound), nil
	}

	skillPath := filepath.Join(skill.Path, skillFileName)
	if err := atomicWrite(skillPath, content); err != nil {
		return nil, err
	}

	result := &Result{
		Success: true,
		Action:  ActionEdit,
		Name:    name,
		Message: fmt.Sprintf("Skill %q updated.", name),
		Path:    skill.Path,
	}
	s.notifyMutation(ctx, name, ActionEdit)
	return result, nil
}

// Delete 删除整个 Skill 目录。
func (s *SkillStore) Delete(ctx context.Context, req DeleteRequest) (*Result, error) {
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

	skill, err := s.Find(ctx, name)
	if err != nil {
		return failure(ActionDelete, name, err.Error(), ErrSkillNotFound), nil
	}

	// Block deletion of built-in (seed) skills tracked in .seed-manifest.
	if isSeed, err := s.isSeedSkill(name); err != nil {
		return nil, fmt.Errorf("check seed manifest: %w", err)
	} else if isSeed {
		return failure(ActionDelete, name,
			fmt.Sprintf("cannot uninstall built-in skill %q", name),
			ErrSkillIsBuiltin), nil
	}

	if err := os.RemoveAll(skill.Path); err != nil {
		return nil, fmt.Errorf("delete skill dir: %w", err)
	}

	result := &Result{
		Success: true,
		Action:  ActionDelete,
		Name:    name,
		Message: fmt.Sprintf("Skill %q deleted.", name),
		Path:    skill.Path,
	}
	s.notifyMutation(ctx, name, ActionDelete)
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
	err := catalog.WalkSkillDirs(s.rootDir, func(subDir, skillPath string) error {
		if !strings.EqualFold(subDir, name) {
			return nil
		}
		found = &Skill{Name: subDir, Path: filepath.Dir(skillPath)}
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

// isSeedSkill checks whether the given skill name appears in the .seed-manifest
// file located in the store's rootDir. Matching is case-insensitive, consistent
// with Find(). Returns false if the manifest file does not exist.
func (s *SkillStore) isSeedSkill(name string) (bool, error) {
	manifestPath := filepath.Join(s.rootDir, seedManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read seed manifest: %w", err)
	}
	entries, warnings := catalog.ParseSeedManifest(data)
	for _, w := range warnings {
		logs.Warnf("%s", w)
	}
	for entryName := range entries {
		if strings.EqualFold(entryName, name) {
			return true, nil
		}
	}
	return false, nil
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

	manifest, body, err := catalog.ParseDocument([]byte(content))
	if err != nil {
		return fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}

	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("frontmatter must include name")
	}
	if strings.TrimSpace(manifest.Description) == "" {
		return fmt.Errorf("frontmatter must include description")
	}
	if len(manifest.Description) > maxDescriptionLength {
		return fmt.Errorf("description exceeds %d characters", maxDescriptionLength)
	}
	if strings.TrimSpace(body) == "" {
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
	for current != root {
		rel, err := filepath.Rel(root, current)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return
		}
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

func failure(action string, name string, message string, cause *SkillError) *Result {
	r := &Result{
		Success: false,
		Action:  action,
		Name:    name,
		Error:   message,
	}
	if cause != nil {
		r.ErrorCode = cause.Code
	}
	return r
}

const skillMetadataFile = ".skill-metadata"

// SkillMetadata stores the source and skill_id from the marketplace.
type SkillMetadata struct {
	Source  string `json:"source"`
	SkillID string `json:"skill_id"`
}

func writeSkillMetadata(skillDir, source, skillID string) error {
	data, err := json.Marshal(SkillMetadata{Source: source, SkillID: skillID})
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(skillDir, skillMetadataFile), string(data))
}

// ReadSkillMetadata reads the .skill-metadata file from the given skill directory.
func ReadSkillMetadata(skillDir string) (*SkillMetadata, error) {
	path := filepath.Join(skillDir, skillMetadataFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m SkillMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
