// Package skillarchive validates and extracts untrusted Skill zip bundles.
package skillarchive

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/insmtx/Leros/backend/internal/skill/catalog"
)

const (
	// MaxPackageBytes bounds the downloaded compressed archive.
	MaxPackageBytes   int64 = 64 << 20
	maxExtractedBytes int64 = 256 << 20
	maxFiles                = 2_000
	maxSkillMDBytes   int64 = 1 << 20
)

// Validate verifies archive structure before it is installed or extracted.
func Validate(zipBytes []byte) error {
	if int64(len(zipBytes)) > MaxPackageBytes {
		return fmt.Errorf("skill package exceeds %d byte limit", MaxPackageBytes)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	return validateFiles(reader.File)
}

// Extract validates zipBytes then writes a regular-file-only bundle into dest.
// dest must be new or empty and be on the same filesystem as its final rename.
func Extract(zipBytes []byte, dest string) error {
	if err := Validate(zipBytes); err != nil {
		return err
	}
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("create skill extraction directory: %w", err)
	}
	var extracted int64
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := filepath.FromSlash(file.Name)
		target := filepath.Join(dest, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create skill entry parent: %w", err)
		}
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("open zip entry %q: %w", file.Name, err)
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			rc.Close()
			return fmt.Errorf("create skill entry %q: %w", file.Name, err)
		}
		remaining := maxExtractedBytes - extracted
		written, copyErr := io.Copy(out, io.LimitReader(rc, remaining+1))
		closeErr := out.Close()
		rc.Close()
		extracted += written
		if copyErr != nil {
			return fmt.Errorf("extract zip entry %q: %w", file.Name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close skill entry %q: %w", file.Name, closeErr)
		}
		if extracted > maxExtractedBytes {
			return fmt.Errorf("skill package extraction exceeds %d byte limit", maxExtractedBytes)
		}
	}
	return nil
}

func validateFiles(files []*zip.File) error {
	if len(files) == 0 {
		return fmt.Errorf("zip does not contain SKILL.md")
	}
	if len(files) > maxFiles {
		return fmt.Errorf("skill package has too many files")
	}
	var total int64
	skillCount := 0
	regularPaths := make(map[string]struct{}, len(files))
	for _, file := range files {
		if err := validatePath(file.Name); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		mode := file.Mode()
		if mode&os.ModeType != 0 {
			return fmt.Errorf("invalid zip entry %q: links and special files are not allowed", file.Name)
		}
		cleanPath := filepath.ToSlash(filepath.Clean(file.Name))
		if !utf8.ValidString(cleanPath) {
			return fmt.Errorf("invalid zip entry: path is not valid UTF-8")
		}
		if _, exists := regularPaths[cleanPath]; exists {
			return fmt.Errorf("zip contains duplicate entry %q", cleanPath)
		}
		regularPaths[cleanPath] = struct{}{}
		if file.UncompressedSize64 > uint64(maxExtractedBytes-total) {
			return fmt.Errorf("skill package extraction exceeds %d byte limit", maxExtractedBytes)
		}
		total += int64(file.UncompressedSize64)
		if strings.EqualFold(filepath.Base(file.Name), "SKILL.md") {
			skillCount++
			if skillCount > 1 {
				return fmt.Errorf("zip contains duplicate SKILL.md")
			}
			if file.UncompressedSize64 > uint64(maxSkillMDBytes) {
				return fmt.Errorf("SKILL.md exceeds %d byte limit", maxSkillMDBytes)
			}
			rc, err := file.Open()
			if err != nil {
				return fmt.Errorf("open zip entry %q: %w", file.Name, err)
			}
			raw, readErr := io.ReadAll(io.LimitReader(rc, maxSkillMDBytes+1))
			rc.Close()
			if readErr != nil {
				return fmt.Errorf("read zip entry %q: %w", file.Name, readErr)
			}
			if int64(len(raw)) > maxSkillMDBytes {
				return fmt.Errorf("SKILL.md exceeds %d byte limit", maxSkillMDBytes)
			}
			if _, body, err := catalog.ParseDocument(raw); err != nil || strings.TrimSpace(body) == "" {
				if err != nil {
					return fmt.Errorf("SKILL.md in zip is invalid: %w", err)
				}
				return fmt.Errorf("SKILL.md in zip is invalid: content is required")
			}
		}
	}
	if skillCount != 1 {
		return fmt.Errorf("zip does not contain SKILL.md")
	}
	return nil
}

func validatePath(name string) error {
	if name == "" || strings.Contains(name, "\\") || filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("invalid zip entry: absolute path detected (%q)", name)
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("invalid zip entry: path traversal detected (%q)", name)
	}
	return nil
}
