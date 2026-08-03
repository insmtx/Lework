package skillsync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	skillarchive "github.com/insmtx/Leros/backend/internal/skill/archive"
	skillcache "github.com/insmtx/Leros/backend/internal/skill/cache"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
)

func buildPackage(root, code string) ([]byte, error) {
	code, err := validSkillCode(code)
	if err != nil {
		return nil, err
	}
	skillRoot := filepath.Join(root, code)
	skillDocument, err := os.ReadFile(filepath.Join(skillRoot, "SKILL.md"))
	if err != nil {
		return nil, fmt.Errorf("read Skill %q document: %w", code, err)
	}
	manifest, body, err := skillcatalog.ParseDocument(skillDocument)
	if err != nil {
		return nil, fmt.Errorf("parse Skill %q document: %w", code, err)
	}
	if manifest == nil || strings.TrimSpace(manifest.Name) != code {
		return nil, fmt.Errorf("Skill %q manifest name must match its directory", code)
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("Skill %q document body is required", code)
	}

	files := make(map[string][]byte)
	var totalBytes int64 = int64(len(skillDocument))
	err = filepath.WalkDir(skillRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(skillRoot, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if ignoredPackagePath(relative, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeType != 0 {
			return fmt.Errorf("Skill %q contains link or special file %q", code, relative)
		}
		if filepath.ToSlash(relative) == "SKILL.md" {
			return nil
		}
		if info.Size() > skillarchive.MaxPackageBytes-totalBytes {
			return fmt.Errorf("Skill %q package source exceeds %d byte limit", code, skillarchive.MaxPackageBytes)
		}
		totalBytes += info.Size()
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = content
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect Skill %q package: %w", code, err)
	}
	archive, err := skillcache.GenerateSkillZip(skillDocument, files)
	if err != nil {
		return nil, fmt.Errorf("generate Skill %q package: %w", code, err)
	}
	if err := skillarchive.Validate(archive); err != nil {
		return nil, fmt.Errorf("validate Skill %q package: %w", code, err)
	}
	return archive, nil
}

func ignoredPackagePath(relative string, directory bool) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts {
		if part == ".git" || part == "__pycache__" || part == ".cache" {
			return true
		}
	}
	base := parts[len(parts)-1]
	if base == ".DS_Store" || strings.HasSuffix(base, ".tmp") {
		return true
	}
	return directory &&
		(strings.HasPrefix(base, ".skill-install-") || strings.HasPrefix(base, ".skill-backup-"))
}
