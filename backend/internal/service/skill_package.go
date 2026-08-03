package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	skillcache "github.com/insmtx/Leros/backend/internal/skill/cache"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
)

type preparedSkillPackage struct {
	Archive  []byte
	Manifest skillcatalog.Manifest
	SHA256   string
	Content  *skillRevisionContentDraft
}

func prepareSkillPackage(rawArchive []byte) (*preparedSkillPackage, error) {
	if err := validateZipSkill(rawArchive); err != nil {
		return nil, err
	}
	normalized, _, err := normalizeSkillArchive(rawArchive)
	if err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(normalized), int64(len(normalized)))
	if err != nil {
		return nil, fmt.Errorf("open normalized Skill package: %w", err)
	}
	files := make(map[string][]byte)
	var skillDocument []byte
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		content, err := readSkillArchiveFile(file)
		if err != nil {
			return nil, err
		}
		cleanName := path.Clean(file.Name)
		if strings.EqualFold(cleanName, skillEntrypointPath) {
			skillDocument = content
		} else {
			if _, exists := files[cleanName]; exists {
				return nil, fmt.Errorf("duplicate normalized Skill path %q", cleanName)
			}
			files[cleanName] = content
		}
	}
	if skillDocument == nil {
		return nil, fmt.Errorf("root SKILL.md is required")
	}
	skillDocument, err = skillcatalog.NormalizeDocument(skillDocument)
	if err != nil {
		return nil, fmt.Errorf("normalize SKILL.md: %w", err)
	}
	if err := validateSkillMDFromBytes(skillDocument); err != nil {
		return nil, err
	}
	manifest, _, err := skillcatalog.ParseDocument(skillDocument)
	if err != nil {
		return nil, fmt.Errorf("parse SKILL.md: %w", err)
	}
	archive, err := skillcache.GenerateSkillZip(skillDocument, files)
	if err != nil {
		return nil, fmt.Errorf("generate standard Skill package: %w", err)
	}
	if err := validateZipSkill(archive); err != nil {
		return nil, fmt.Errorf("validate standard Skill package: %w", err)
	}
	hash := sha256.Sum256(archive)
	artifactSHA := hex.EncodeToString(hash[:])
	content, err := buildSkillRevisionContent(archive, artifactSHA)
	if err != nil {
		return nil, fmt.Errorf("build Skill content snapshot: %w", err)
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Metadata.Category = strings.TrimSpace(manifest.Metadata.Category)
	return &preparedSkillPackage{
		Archive: archive, Manifest: *manifest, SHA256: artifactSHA, Content: content,
	}, nil
}
