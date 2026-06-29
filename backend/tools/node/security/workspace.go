package security

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

// WorkspaceRoot 获取工作区根目录。
func WorkspaceRoot() (string, error) {
	return leros.WorkspaceRoot()
}

// RealWorkspaceRoot 获取工作区根目录的真实路径（解析符号链接）。
func RealWorkspaceRoot() (string, error) {
	root, err := WorkspaceRoot()
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root symlinks: %w", err)
	}
	return filepath.Clean(realRoot), nil
}

// CanonicalPath resolves symlinks through the nearest existing ancestor.
// It also handles paths whose final components do not exist yet.
func CanonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	current := absolute
	for {
		if _, statErr := os.Lstat(current); statErr == nil {
			realAncestor, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return "", fmt.Errorf("resolve path symlinks: %w", evalErr)
			}
			suffix, relErr := filepath.Rel(current, absolute)
			if relErr != nil {
				return "", fmt.Errorf("resolve path suffix: %w", relErr)
			}
			if suffix == "." {
				return filepath.Clean(realAncestor), nil
			}
			return filepath.Clean(filepath.Join(realAncestor, suffix)), nil
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat path: %w", statErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absolute, nil
		}
		current = parent
	}
}
