package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/insmtx/Leros/backend/types"
)

const skillEntrypointPath = "SKILL.md"

type skillRevisionContentDraft struct {
	ArtifactSHA256    string
	EntrypointContent string
	FileIndex         types.PluginRevisionFileList
}

func buildSkillRevisionContent(archive []byte, artifactSHA256 string) (*skillRevisionContentDraft, error) {
	if err := validateZipSkill(archive); err != nil {
		return nil, err
	}
	artifactSHA256, err := normalizedPluginSHA256(artifactSHA256)
	if err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("open skill package: %w", err)
	}

	files := make(types.PluginRevisionFileList, 0, len(reader.File))
	seen := make(map[string]struct{}, len(reader.File))
	var entrypointContent string
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		cleanPath := path.Clean(file.Name)
		if !utf8.ValidString(cleanPath) {
			return nil, fmt.Errorf("skill package entry path is not valid UTF-8")
		}
		if _, exists := seen[cleanPath]; exists {
			return nil, fmt.Errorf("skill package contains duplicate entry %q", cleanPath)
		}
		seen[cleanPath] = struct{}{}

		content, err := readSkillArchiveFile(file)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(content)
		files = append(files, types.PluginRevisionFile{
			Path:      cleanPath,
			SizeBytes: int64(len(content)),
			SHA256:    hex.EncodeToString(sum[:]),
		})
		if strings.EqualFold(cleanPath, skillEntrypointPath) {
			if !utf8.Valid(content) {
				return nil, fmt.Errorf("SKILL.md must be valid UTF-8")
			}
			entrypointContent = string(content)
		}
	}
	if entrypointContent == "" {
		return nil, fmt.Errorf("skill package does not contain root SKILL.md")
	}
	sort.Slice(files, func(left, right int) bool {
		return files[left].Path < files[right].Path
	})
	for index := 0; index+1 < len(files); index++ {
		if strings.HasPrefix(files[index+1].Path, files[index].Path+"/") {
			return nil, fmt.Errorf(
				"skill package entry %q conflicts with file %q",
				files[index+1].Path,
				files[index].Path,
			)
		}
	}
	return &skillRevisionContentDraft{
		ArtifactSHA256:    artifactSHA256,
		EntrypointContent: entrypointContent,
		FileIndex:         files,
	}, nil
}

func (draft *skillRevisionContentDraft) model(
	pluginRevisionID uint,
) *types.PluginRevisionContent {
	return &types.PluginRevisionContent{
		PluginRevisionID:  pluginRevisionID,
		Schema:            types.PluginRevisionContentSchemaSkillV1,
		ArtifactSHA256:    draft.ArtifactSHA256,
		EntrypointPath:    skillEntrypointPath,
		EntrypointContent: draft.EntrypointContent,
		FileIndex:         append(types.PluginRevisionFileList(nil), draft.FileIndex...),
	}
}
