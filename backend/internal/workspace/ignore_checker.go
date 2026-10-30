package workspace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

// ---- Ignore rules ----

// runtimeInternalDirs lists directories that contain runtime-private data
// and should never be published as artifacts.
var runtimeInternalDirs = []string{".opencode", ".leros"}

var attendanceInternalDirs = []string{".attendance-work", ".attendance-pages"}

// IsRuntimeInternalPath reports whether the given relative path belongs to
// a runtime-internal directory that should be excluded from artifacts.
func IsRuntimeInternalPath(relPath string) bool {
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	for _, dir := range runtimeInternalDirs {
		if relPath == dir || strings.HasPrefix(relPath, dir+"/") {
			return true
		}
	}
	return false
}

// IsAttendanceInternalPath reports whether a path belongs to private attendance work data.
func IsAttendanceInternalPath(relPath string) bool {
	relPath = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(strings.TrimSpace(relPath), `\`, "/")))
	for _, dir := range attendanceInternalDirs {
		if relPath == dir || strings.HasPrefix(relPath, dir+"/") {
			return true
		}
	}
	return false
}

func effectiveFinalArtifactPath(repoDir string, item ManifestArtifact) (string, bool, error) {
	if !item.IsFinal {
		return "", false, nil
	}

	value := strings.ReplaceAll(strings.TrimSpace(item.Path), `\`, "/")
	if value == "" || strings.HasPrefix(value, "/") || hasWindowsVolume(value) {
		return "", false, fmt.Errorf("invalid artifact path %q", item.Path)
	}
	path := pathpkg.Clean(value)
	if path == "." || path == ".." || strings.HasPrefix(path, "../") {
		return "", false, fmt.Errorf("invalid artifact path %q", item.Path)
	}
	if IsAttendanceInternalPath(path) {
		return path, false, nil
	}
	if strings.TrimSpace(repoDir) != "" {
		if _, err := SafeJoin(repoDir, path); err != nil {
			return "", false, fmt.Errorf("invalid artifact path %q: %w", item.Path, err)
		}
	}
	return path, true, nil
}

func hasWindowsVolume(path string) bool {
	return len(path) >= 2 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':'
}

// builtinExcludedDirs lists directory base names that should never be scanned
// for artifacts.
var builtinExcludedDirs = map[string]bool{
	"tmp": true, "temp": true, "logs": true, "log": true,
	".cache": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "target": true,
}

// ShouldSkipArtifactFile reports whether a repository-relative file path
// should be excluded from artifact publication. It checks the path against
// builtin excluded dirs and builtin excluded file names.
//
// 必须传仓库相对路径：传绝对路径会把系统临时目录 /tmp 当成被排除的 tmp/，
// 导致位于 /tmp 下的工作区产物被静默丢弃。
func ShouldSkipArtifactFile(relPath string) bool {
	relPath = pathpkg.Clean(filepath.ToSlash(strings.TrimSpace(relPath)))
	for _, part := range strings.Split(relPath, "/") {
		if builtinExcludedDirs[strings.ToLower(part)] {
			return true
		}
	}
	base := pathpkg.Base(relPath)
	if base == ".DS_Store" || base == "Thumbs.db" || base == ".gitignore" {
		return true
	}
	ext := filepath.Ext(base)
	if ext == ".swp" || ext == ".swo" || ext == ".log" {
		return true
	}
	return false
}

// ---- Manifest helpers ----

// manifestHasFinalEntries checks whether the manifest already contains at least one final entry.
func manifestHasFinalEntries(manifestPath, repoDir string) (bool, error) {
	paths, err := manifestFinalPaths(manifestPath, repoDir)
	return len(paths) > 0, err
}

func manifestFinalPaths(manifestPath, repoDir string) (map[string]bool, error) {
	paths := make(map[string]bool)
	file, err := os.Open(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return paths, nil
		}
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry ManifestArtifact
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("parse artifact manifest line %d: %w", lineNumber, err)
		}
		path, eligible, err := effectiveFinalArtifactPath(repoDir, entry)
		if err != nil {
			return nil, err
		}
		if eligible {
			paths[path] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read artifact manifest: %w", err)
	}
	return paths, nil
}
