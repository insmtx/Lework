package filestore

import (
	"fmt"
	"path/filepath"
	"strings"
)

var composerAllowedExtensions = map[string]struct{}{
	".pdf":      {},
	".doc":      {},
	".docx":     {},
	".xls":      {},
	".xlsx":     {},
	".ppt":      {},
	".pptx":     {},
	".md":       {},
	".markdown": {},
	".html":     {},
	".htm":      {},
	".png":      {},
	".jpg":      {},
	".jpeg":     {},
	".gif":      {},
	".bmp":      {},
	".webp":     {},
	".svg":      {},
	".txt":      {},
}

// ValidateComposerUploadFilename checks whether the local-path suffix is allowed.
func ValidateComposerUploadFilename(filename string) error {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	if ext == "" {
		return fmt.Errorf("unsupported file type")
	}
	if _, ok := composerAllowedExtensions[ext]; !ok {
		return fmt.Errorf("unsupported file type")
	}
	return nil
}
