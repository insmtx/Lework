package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorkspacePath 解析相对于工作区的路径，检查是否超出工作区
func ResolveWorkspacePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	root, err := RealWorkspaceRoot()
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	resolved := filepath.Clean(path)
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(root, resolved)
	}
	resolved, err = CanonicalPath(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path relative to workspace: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path is outside workspace: %s", path)
	}

	return resolved, nil
}

// PathWithinWorkspace 检查路径是否在工作区内
func PathWithinWorkspace(path string) error {
	root, err := RealWorkspaceRoot()
	if err != nil {
		return err
	}
	absPath, err := CanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return fmt.Errorf("resolve path relative to workspace: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path is outside workspace: %s", path)
	}
	return nil
}

// ResolveExistingWorkspacePath 解析路径并验证符号链接目标在工作区内
func ResolveExistingWorkspacePath(path string) (string, error) {
	resolved, err := ResolveWorkspacePath(path)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path symlinks: %w", err)
	}
	if err := PathWithinWorkspace(realPath); err != nil {
		return "", err
	}
	return realPath, nil
}

// ValidateWritableWorkspacePath 验证路径可用于写入，检查符号链接和父目录
func ValidateWritableWorkspacePath(path string) error {
	resolved, err := ResolveWorkspacePath(path)
	if err != nil {
		return err
	}

	parent := filepath.Dir(resolved)
	ancestor, err := NearestExistingAncestor(parent)
	if err != nil {
		return err
	}
	realAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return fmt.Errorf("resolve parent directory symlinks: %w", err)
	}
	if err := PathWithinWorkspace(realAncestor); err != nil {
		return err
	}

	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("create node file parent directory: %w", err)
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return fmt.Errorf("resolve parent directory symlinks: %w", err)
	}
	if err := PathWithinWorkspace(realParent); err != nil {
		return err
	}

	info, err := os.Lstat(resolved)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check node file: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}

	realPath, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return fmt.Errorf("resolve file symlink: %w", err)
	}
	return PathWithinWorkspace(realPath)
}

// NearestExistingAncestor 查找路径最近的已存在的祖先目录
func NearestExistingAncestor(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		if _, err := os.Lstat(current); err == nil {
			return current, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("check parent directory: %w", err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing parent directory for path: %s", path)
		}
		current = parent
	}
}
