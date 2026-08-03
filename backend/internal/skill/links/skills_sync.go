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

// SyncToLerosDir copies worker built-in skills to the protected worker system
// cache (.leros/skills/.system). Run views link from this directory explicitly.
func SyncToLerosDir(sourceDir string) error {
	sourceDir, err := ResolveBuiltinSkillsSource(sourceDir, "worker")
	if err != nil {
		return err
	}

	userDir, err := leros.JoinWorkspace(".leros", "skills", ".system")
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

// ResolveBuiltinSkillsSource resolves the built-in skills directory for a given subdir (e.g. "server" or "worker").
// Priority: 1. sourceDir param, 2. LEROS_SKILLS_DIR env, 3. default locations.
func ResolveBuiltinSkillsSource(sourceDir string, subdir string) (string, error) {
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

func resolveBuiltinSkillsSource(sourceDir string, subdir string) (string, error) {
	return ResolveBuiltinSkillsSource(sourceDir, subdir)
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

// seedManifestFile is the manifest file tracking synced skill directory hashes.
const seedManifestFile = ".seed-manifest"

// syncSkillDir synchronizes protected worker built-ins and tracks their hashes.
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
