// Package cache provides skill archive utilities shared by plugin import and installation paths.
package cache

import (
	"archive/zip"
	"bytes"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/insmtx/Leros/backend/internal/skill/fetch"
)

// GenerateSkillZip creates a standard Skill zip archive from the SKILL.md content and
// additional files. It filters macOS junk paths automatically.
func GenerateSkillZip(content []byte, files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	if err := writeZipEntry(zw, "SKILL.md", content); err != nil {
		return nil, fmt.Errorf("write SKILL.md to zip: %w", err)
	}

	paths := make([]string, 0, len(files))
	for relPath := range files {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	for _, relPath := range paths {
		data := files[relPath]
		if fetch.IsMacOSJunkPath(relPath) {
			continue
		}
		if err := writeZipEntry(zw, filepath.ToSlash(relPath), data); err != nil {
			return nil, fmt.Errorf("write %s to zip: %w", relPath, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip writer: %w", err)
	}
	return buf.Bytes(), nil
}

func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
