package skilllinks

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/insmtx/Leros/backend/internal/skill/catalog"
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

// seedManifestFile is the manifest file tracking synced skill directory hashes.
const seedManifestFile = ".seed-manifest"

// syncSkillDir synchronizes skill directories from source to target using directory-level
// SHA256 hashes tracked in a .seed-manifest file. It detects additions, modifications,
// and deletions of entire skill directories.
func syncSkillDir(sourceDir string, targetDir string) error {
	skillDirs, err := listSkillDirs(sourceDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	manifestPath := filepath.Join(targetDir, seedManifestFile)
	oldManifest, err := readSeedManifest(manifestPath)
	if err != nil {
		logs.Warnf("Failed to read seed manifest, will resync all: %v", err)
		oldManifest = make(map[string]string)
	}

	newManifest := make(map[string]string, len(skillDirs))
	for _, skillName := range skillDirs {
		sourceSkillDir := filepath.Join(sourceDir, skillName)
		sourceHash, err := computeDirHash(sourceSkillDir)
		if err != nil {
			return fmt.Errorf("compute hash for %s: %w", skillName, err)
		}
		newManifest[skillName] = sourceHash

		oldHash := oldManifest[skillName]
		if sourceHash != oldHash {
			logs.Infof("skill %s changed (old=%s, new=%s), syncing...", skillName, oldHash, sourceHash)
			if err := copyDir(sourceSkillDir, filepath.Join(targetDir, skillName)); err != nil {
				return fmt.Errorf("copy skill %s: %w", skillName, err)
			}
		} else {
			logs.Debugf("skill %s unchanged, skipping", skillName)
		}
	}

	// Remove stale skills that exist in manifest but not in source.
	for oldName := range oldManifest {
		if _, ok := newManifest[oldName]; !ok {
			logs.Infof("skill %s removed from source, cleaning up", oldName)
			if err := os.RemoveAll(filepath.Join(targetDir, oldName)); err != nil {
				logs.Warnf("Failed to remove stale skill %s: %v", oldName, err)
			}
		}
	}

	if err := writeSeedManifest(manifestPath, newManifest); err != nil {
		return fmt.Errorf("write seed manifest: %w", err)
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

// computeDirHash computes a deterministic SHA256 hash for an entire directory.
// Files are walked in sorted order; each file contributes sha256(relPath + \x00 + content).
// The final hash is sha256(concat of all per-file hex hashes).
func computeDirHash(dirPath string) (string, error) {
	var fileHashes []string
	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(dirPath, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		h.Write([]byte(relPath))
		h.Write([]byte{0})
		h.Write(data)
		fileHashes = append(fileHashes, fmt.Sprintf("%x", h.Sum(nil)))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(fileHashes)
	combined := sha256.New()
	for _, fh := range fileHashes {
		combined.Write([]byte(fh))
	}
	return fmt.Sprintf("%x", combined.Sum(nil)), nil
}

// readSeedManifest reads a .seed-manifest file and returns a map of skillName → hash.
// Returns an empty map if the file does not exist.
func readSeedManifest(manifestPath string) (map[string]string, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	entries, warnings := catalog.ParseSeedManifest(data)
	for _, w := range warnings {
		logs.Warnf("%s", w)
	}
	return entries, nil
}

// writeSeedManifest writes a .seed-manifest file atomically (tmp + rename).
// Entries are written sorted by skill name for determinism.
func writeSeedManifest(manifestPath string, entries map[string]string) error {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	for _, name := range names {
		fmt.Fprintf(&buf, "%s:%s\n", name, entries[name])
	}

	tmpPath := manifestPath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, manifestPath)
}

// copyDir copies an entire directory tree from src to dst atomically.
// Files are first copied to a temporary directory alongside dst, then
// the old dst is removed and the tmp directory is renamed into place.
func copyDir(src, dst string) error {
	tmpDst := dst + ".tmp"
	// Clean up any stale tmp dir from a previous failed attempt.
	if err := os.RemoveAll(tmpDst); err != nil {
		return fmt.Errorf("remove stale tmp dir %s: %w", tmpDst, err)
	}

	if err := filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(tmpDst, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, info.Mode().Perm())
	}); err != nil {
		os.RemoveAll(tmpDst) // best-effort cleanup
		return err
	}

	// Atomically swap: remove old target, rename tmp into place.
	if err := os.RemoveAll(dst); err != nil {
		os.RemoveAll(tmpDst)
		return fmt.Errorf("remove target dir %s: %w", dst, err)
	}
	if err := os.Rename(tmpDst, dst); err != nil {
		os.RemoveAll(tmpDst)
		return fmt.Errorf("rename tmp dir to %s: %w", dst, err)
	}
	return nil
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
