package provider

import (
	"fmt"
	"os"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

// ResolveRunWorkDir returns the configured run workdir or the workspace temp fallback.
func ResolveRunWorkDir(workDir string) (string, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir != "" {
		return workDir, nil
	}
	tempDir, err := leros.TempDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", fmt.Errorf("create workspace temp dir: %w", err)
	}
	return tempDir, nil
}
