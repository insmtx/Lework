package engines

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/ygpkg/yg-go/logs"
)

const skillManifestFile = "SKILL.md"

var errNoSkillDirs = errors.New("no skill directories found")

// SyncToLerosDir copies worker built-in skills to the Leros workspace skills directory (.leros/skills).
func SyncToLerosDir(sourceDir string) error {
	sourceDir, err := resolveBuiltinSkillsSource(sourceDir, "worker")
	if err != nil {
		return err
	}

	userDir, err := defaultLerosSkillsDir()
	if err != nil {
		return err
	}

	resolvedUserDir, err := expandPath(userDir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(resolvedUserDir, 0o755); err != nil {
		return fmt.Errorf("create workspace skills directory: %w", err)
	}

	logs.Infof("Syncing worker built-in skills from %s to %s", sourceDir, resolvedUserDir)
	return syncSkillDir(sourceDir, resolvedUserDir)
}

// SyncServerSkillsDir copies server built-in skills to the workspace skills directory ({workspace}/skills).
func SyncServerSkillsDir(sourceDir string) error {
	sourceDir, err := resolveBuiltinSkillsSource(sourceDir, "server")
	if err != nil {
		return err
	}

	targetDir, err := leros.JoinWorkspace("skills")
	if err != nil {
		return err
	}

	resolvedTargetDir, err := expandPath(targetDir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(resolvedTargetDir, 0o755); err != nil {
		return fmt.Errorf("create server skills directory: %w", err)
	}

	logs.Infof("Syncing server built-in skills from %s to %s", sourceDir, resolvedTargetDir)
	return syncSkillDir(sourceDir, resolvedTargetDir)
}

// ReconcileExternalSkillLinks 全量对齐外部 CLI skill 目录与 .leros/skills。
// 遍历 .leros/skills 下所有合法 skill 子目录，在每个外部 CLI skill 根目录下创建
// {skillName} → .leros/skills/{skillName} 的 symlink。
//
// 同名目标处理规则（由 ensureSymlink 实现）：
//   - 不存在：创建 symlink。
//   - 正确 symlink：跳过（幂等）。
//   - 错误 symlink：删除并重建。
//   - 真实目录或文件：删除并替换为 symlink。
//
// 安全：删除前使用 Lstat，不跟随 symlink。仅删除 external/{skillName} 子路径，
// 不删除外部根目录或 .leros/skills 源目录。
// 适用场景：worker 启动 / bootstrap 时的全量初始化。
func ReconcileExternalSkillLinks(cliSkillDirs []string) error {
	userDir, err := defaultLerosSkillsDir()
	if err != nil {
		return err
	}

	resolvedUserDir, err := expandPath(userDir)
	if err != nil {
		return err
	}

	// Check if workspace skills directory exists.
	if _, err := os.Stat(resolvedUserDir); os.IsNotExist(err) {
		logs.Debugf("Leros workspace skills directory does not exist, skipping sync to external CLI: %s", resolvedUserDir)
		return nil
	}

	if len(cliSkillDirs) == 0 {
		logs.Debug("No external CLI skill directories provided, skipping sync")
		return nil
	}

	skillNames, err := listSkillDirs(resolvedUserDir)
	if err != nil {
		if errors.Is(err, errNoSkillDirs) {
			logs.Debugf("No skills found in %s, skipping sync to external CLI", resolvedUserDir)
			return nil
		}
		return err
	}

	for _, cliDir := range cliSkillDirs {
		resolvedCliDir, err := expandPath(cliDir)
		if err != nil {
			logs.Warnf("Failed to resolve CLI skill directory %s: %v", cliDir, err)
			continue
		}

		logs.Infof("Syncing skills from %s to %s via symlinks", resolvedUserDir, resolvedCliDir)

		if err := os.MkdirAll(resolvedCliDir, 0o755); err != nil {
			logs.Warnf("Failed to create external CLI skill directory %s: %v", resolvedCliDir, err)
			continue
		}

		for _, skillName := range skillNames {
			sourcePath := filepath.Join(resolvedUserDir, skillName)
			targetPath := filepath.Join(resolvedCliDir, skillName)

			if err := ensureSymlink(sourcePath, targetPath); err != nil {
				logs.Warnf("Failed to sync skill %s to %s: %v", skillName, targetPath, err)
				continue
			}
		}
	}

	return nil
}

// EnsureExternalSkillLink 为单个 skill 在所有外部 CLI 目录下创建或替换 symlink。
// 与 ReconcileExternalSkillLinks 不同，本函数只处理一个 skill，用于 create 后增量维护。
// 限定条件：skillName 不能包含路径分隔符、不能是绝对路径、不能是 ".."。
// 若 .leros/skills/{skillName} 不存在，返回错误。
func EnsureExternalSkillLink(skillName string, cliSkillDirs []string) error {
	if strings.TrimSpace(skillName) == "" {
		return fmt.Errorf("skill name is required")
	}
	if strings.ContainsAny(skillName, "/\\") || filepath.IsAbs(skillName) || skillName == ".." {
		return fmt.Errorf("invalid skill name %q: must not contain path separators or be absolute", skillName)
	}

	userDir, err := defaultLerosSkillsDir()
	if err != nil {
		return err
	}
	resolvedUserDir, err := expandPath(userDir)
	if err != nil {
		return err
	}

	sourcePath := filepath.Join(resolvedUserDir, skillName)
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("source skill %s does not exist in .leros/skills: %w", skillName, err)
	}

	for _, cliDir := range cliSkillDirs {
		resolvedCliDir, err := expandPath(cliDir)
		if err != nil {
			logs.Warnf("Failed to resolve CLI skill directory %s: %v", cliDir, err)
			continue
		}
		if err := os.MkdirAll(resolvedCliDir, 0o755); err != nil {
			logs.Warnf("Failed to create external CLI skill directory %s: %v", resolvedCliDir, err)
			continue
		}
		targetPath := filepath.Join(resolvedCliDir, skillName)
		if err := ensureSymlink(sourcePath, targetPath); err != nil {
			logs.Warnf("Failed to sync skill %s to %s: %v", skillName, targetPath, err)
			continue
		}
	}

	return nil
}

// RemoveExternalSkillLink removes external symlinks for a single skill from all CLI directories.
// Only symlinks are removed; real directories or files are left untouched.
// Logs warnings on failures but continues to the next directory (same pattern as EnsureExternalSkillLink).
// No-op if the symlink does not exist.
func RemoveExternalSkillLink(skillName string, cliSkillDirs []string) error {
	if strings.TrimSpace(skillName) == "" {
		return fmt.Errorf("skill name is required")
	}
	if strings.ContainsAny(skillName, "/\\") || filepath.IsAbs(skillName) || skillName == ".." {
		return fmt.Errorf("invalid skill name %q: must not contain path separators or be absolute", skillName)
	}

	for _, cliDir := range cliSkillDirs {
		resolvedCliDir, err := expandPath(cliDir)
		if err != nil {
			logs.Warnf("Failed to resolve CLI skill directory %s: %v", cliDir, err)
			continue
		}
		targetPath := filepath.Join(resolvedCliDir, skillName)
		fi, err := os.Lstat(targetPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Nothing to remove, no-op
			}
			logs.Warnf("Failed to stat external symlink %s: %v", targetPath, err)
			continue
		}
		// Only remove symlinks, never real directories or regular files.
		if fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if err := os.Remove(targetPath); err != nil {
			logs.Warnf("Failed to remove external symlink %s: %v", targetPath, err)
			continue
		}
	}
	return nil
}

// resolveBuiltinSkillsSource resolves the built-in skills directory for a given subdir (e.g. "server" or "worker").
// Priority: 1. sourceDir param, 2. LEROS_SKILLS_DIR env, 3. default locations.
func resolveBuiltinSkillsSource(sourceDir string, subdir string) (string, error) {
	skillsRelPath := filepath.Join("backend", "skills", subdir)
	var candidates []string
	if strings.TrimSpace(sourceDir) != "" {
		candidates = append([]string{sourceDir}, candidates...)
	}
	if configured := strings.TrimSpace(os.Getenv("LEROS_SKILLS_DIR")); configured != "" {
		candidates = append([]string{configured}, candidates...)
	}
	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, findParentDirCandidates(workingDir, skillsRelPath)...)
	}
	if executablePath, err := os.Executable(); err == nil {
		candidates = append(candidates, findParentDirCandidates(filepath.Dir(executablePath), skillsRelPath)...)
	}
	candidates = append(candidates, filepath.Join(string(os.PathSeparator), "app", skillsRelPath))

	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("built-in skills directory not found")
}

func findParentDirCandidates(startDir string, relativePath string) []string {
	var candidates []string
	current := filepath.Clean(startDir)
	for {
		candidates = append(candidates, filepath.Join(current, relativePath))
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return candidates
}

func defaultLerosSkillsDir() (string, error) {
	return leros.SkillsDir()
}

// ensureSymlink ensures target is a symlink pointing to source.
func ensureSymlink(sourcePath string, targetPath string) error {
	fi, err := os.Lstat(targetPath)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			existingTarget, readErr := os.Readlink(targetPath)
			if readErr == nil && existingTarget == sourcePath {
				return nil
			}
		}
		if removeErr := os.RemoveAll(targetPath); removeErr != nil {
			return fmt.Errorf("remove existing %s: %w", targetPath, removeErr)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("lstat %s: %w", targetPath, err)
	}

	if err := os.Symlink(sourcePath, targetPath); err != nil {
		return fmt.Errorf("symlink %s -> %s: %w", targetPath, sourcePath, err)
	}
	return nil
}

func syncSkillDir(sourceDir string, targetDir string) error {
	skillDirs, err := listSkillDirs(sourceDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	for _, skillDir := range skillDirs {
		if err := syncSingleSkillDir(sourceDir, targetDir, skillDir); err != nil {
			return err
		}
	}
	return nil
}

func listSkillDirs(sourceDir string) ([]string, error) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, err
	}

	var skillDirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			logs.Debugf("Skipping non-directory entry in skills root: %s", filepath.Join(sourceDir, entry.Name()))
			continue
		}
		manifestPath := filepath.Join(sourceDir, entry.Name(), skillManifestFile)
		info, err := os.Stat(manifestPath)
		if err != nil {
			logs.Debugf("Skipping directory without %s: %s", skillManifestFile, filepath.Join(sourceDir, entry.Name()))
			continue
		}
		if info.IsDir() {
			logs.Debugf("Skipping directory where %s is a directory: %s", skillManifestFile, filepath.Join(sourceDir, entry.Name()))
			continue
		}
		skillDirs = append(skillDirs, entry.Name())
	}
	if len(skillDirs) == 0 {
		return nil, fmt.Errorf("%w in %s", errNoSkillDirs, sourceDir)
	}
	return skillDirs, nil
}

func syncSingleSkillDir(sourceDir string, targetDir string, skillDir string) error {
	skillSourceDir := filepath.Join(sourceDir, skillDir)
	return filepath.WalkDir(skillSourceDir, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(sourceDir, sourcePath)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, relPath)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		return copyFileIfChanged(sourcePath, targetPath, entry)
	})
}

func copyFileIfChanged(sourcePath string, targetPath string, entry fs.DirEntry) error {
	info, err := entry.Info()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported non-regular skill file %s", sourcePath)
	}

	same, err := sameFileContent(sourcePath, targetPath)
	if err != nil {
		return err
	}
	if same {
		return os.Chmod(targetPath, info.Mode().Perm())
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return err
	}
	return target.Chmod(info.Mode().Perm())
}

func sameFileContent(sourcePath string, targetPath string) (bool, error) {
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if targetInfo.IsDir() {
		return false, fmt.Errorf("target path %s is a directory", targetPath)
	}

	sourceHash, err := fileSHA256(sourcePath)
	if err != nil {
		return false, err
	}
	targetHash, err := fileSHA256(targetPath)
	if err != nil {
		return false, err
	}
	return bytes.Equal(sourceHash, targetHash), nil
}

func fileSHA256(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	hashValue := sha256.New()
	if _, err := io.Copy(hashValue, file); err != nil {
		return nil, err
	}
	return hashValue.Sum(nil), nil
}

func expandPath(pathValue string) (string, error) {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return "", fmt.Errorf("path is required")
	}
	if pathValue == "~" || strings.HasPrefix(pathValue, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if pathValue == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(pathValue, "~/")), nil
	}
	return pathValue, nil
}
