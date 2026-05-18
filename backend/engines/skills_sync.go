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

// SyncToLerosDir copies built-in skills from sourceDir to Leros user skills directory (~/.leros/skills).
// This is the first step in the new sync flow: internal skills -> ~/.leros/skills.
func SyncToLerosDir(sourceDir string) error {
	sourceDir, err := resolveBuiltinSkillsSource(sourceDir)
	if err != nil {
		return err
	}

	userDir, err := defaultLerosSkillsDir()
	if err != nil {
		return err
	}

	// Resolve the user directory path (expand ~)
	resolvedUserDir, err := expandPath(userDir)
	if err != nil {
		return err
	}

	// Create the user skills directory if it doesn't exist
	if err := os.MkdirAll(resolvedUserDir, 0o755); err != nil {
		return fmt.Errorf("create user skills directory: %w", err)
	}

	logs.Infof("Syncing built-in skills from %s to %s", sourceDir, resolvedUserDir)
	return syncSkillDir(sourceDir, resolvedUserDir)
}

// SyncFromLerosToExternal copies skills from Leros user directory (~/.leros/skills) to external CLI skill directories.
// This is the second step in the new sync flow: ~/.leros/skills -> external CLI skill dirs.
func SyncFromLerosToExternal(cliSkillDirs []string) error {
	userDir, err := defaultLerosSkillsDir()
	if err != nil {
		return err
	}

	resolvedUserDir, err := expandPath(userDir)
	if err != nil {
		return err
	}

	// Check if user skills directory exists
	if _, err := os.Stat(resolvedUserDir); os.IsNotExist(err) {
		logs.Debugf("Leros user skills directory does not exist, skipping sync to external CLI: %s", resolvedUserDir)
		return nil
	}

	if len(cliSkillDirs) == 0 {
		logs.Debug("No external CLI skill directories provided, skipping sync")
		return nil
	}

	for _, cliDir := range cliSkillDirs {
		resolvedCliDir, err := expandPath(cliDir)
		if err != nil {
			logs.Warnf("Failed to resolve CLI skill directory %s: %v", cliDir, err)
			continue
		}

		logs.Infof("Syncing skills from %s to %s", resolvedUserDir, resolvedCliDir)
		if err := syncSkillDir(resolvedUserDir, resolvedCliDir); err != nil {
			if errors.Is(err, errNoSkillDirs) {
				logs.Debugf("No skills found in %s to sync to %s", resolvedUserDir, resolvedCliDir)
				continue
			}
			logs.Warnf("Failed to sync skills to %s: %v", resolvedCliDir, err)
		}
	}

	return nil
}

// resolveBuiltinSkillsSource resolves the built-in skills directory from various sources.
// Priority: 1. sourceDir param, 2. LEROS_SKILLS_DIR env, 3. default locations.
func resolveBuiltinSkillsSource(sourceDir string) (string, error) {
	var candidates []string
	if strings.TrimSpace(sourceDir) != "" {
		candidates = append([]string{sourceDir}, candidates...)
	}
	if configured := strings.TrimSpace(os.Getenv("LEROS_SKILLS_DIR")); configured != "" {
		candidates = append([]string{configured}, candidates...)
	}
	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, findParentDirCandidates(workingDir, filepath.Join("backend", "skills"))...)
	}
	if executablePath, err := os.Executable(); err == nil {
		candidates = append(candidates, findParentDirCandidates(filepath.Dir(executablePath), filepath.Join("backend", "skills"))...)
	}
	candidates = append(candidates, filepath.Join(string(os.PathSeparator), "app", "backend", "skills"))

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
			return nil, fmt.Errorf("invalid skill source entry %s: expected skill directory", filepath.Join(sourceDir, entry.Name()))
		}
		manifestPath := filepath.Join(sourceDir, entry.Name(), skillManifestFile)
		info, err := os.Stat(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("invalid skill directory %s: missing %s", filepath.Join(sourceDir, entry.Name()), skillManifestFile)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("invalid skill directory %s: %s must be a file", filepath.Join(sourceDir, entry.Name()), skillManifestFile)
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
