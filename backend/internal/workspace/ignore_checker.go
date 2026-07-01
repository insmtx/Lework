package workspace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---- Ignore rules ----

// runtimeInternalDirs lists directories that contain runtime-private data
// and should never be published as artifacts.
var runtimeInternalDirs = []string{".opencode", ".leros"}

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

// builtinExcludedDirs lists directory base names that should never be scanned
// for artifacts.
var builtinExcludedDirs = map[string]bool{
	"tmp": true, "temp": true, "logs": true, "log": true,
	".cache": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, "target": true,
}

// ShouldSkipArtifactFile reports whether an absolute file path should be
// excluded from artifact publication. It checks the path against builtin
// excluded dirs and builtin excluded file names.
func ShouldSkipArtifactFile(absPath string) bool {
	for _, part := range strings.Split(filepath.ToSlash(absPath), "/") {
		if builtinExcludedDirs[strings.ToLower(part)] {
			return true
		}
	}
	base := filepath.Base(absPath)
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
func manifestHasFinalEntries(manifestPath string) (bool, error) {
	file, err := os.Open(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry ManifestArtifact
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.IsFinal {
			return true, nil
		}
	}
	return false, scanner.Err()
}
