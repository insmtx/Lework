package nodetools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/tools"
	"github.com/insmtx/Leros/backend/tools/node/security"
)

func resolveToolWorkDir(ctx context.Context, requested string) (string, error) {
	base, err := toolWorkDir(ctx)
	if err != nil {
		return "", err
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return base, nil
	}
	return resolveUnderToolWorkDir(ctx, requested)
}

func resolveToolPath(ctx context.Context, path string) (string, error) {
	return resolveUnderToolWorkDir(ctx, path)
}

func resolveExistingToolPath(ctx context.Context, path string) (string, error) {
	resolved, err := resolveToolPath(ctx, path)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path symlinks: %w", err)
	}
	base, err := toolWorkDir(ctx)
	if err != nil {
		return "", err
	}
	if err := ensureInsideBase(base, realPath); err != nil {
		return "", err
	}
	if err := security.PathWithinWorkspace(realPath); err != nil {
		return "", err
	}
	return realPath, nil
}

func validateWritableToolPath(ctx context.Context, path string) error {
	resolved, err := resolveToolPath(ctx, path)
	if err != nil {
		return err
	}
	if err := security.ValidateWritableWorkspacePath(resolved); err != nil {
		return err
	}
	return ensureInsideBaseForWrite(ctx, resolved)
}

func resolveUnderToolWorkDir(ctx context.Context, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	base, err := toolWorkDir(ctx)
	if err != nil {
		return "", err
	}
	candidate := filepath.Clean(path)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if err := ensureInsideBase(base, candidate); err != nil {
		return "", err
	}
	return security.ResolveWorkspacePath(candidate)
}

func toolWorkDir(ctx context.Context) (string, error) {
	if toolCtx, ok := tools.ToolContextFrom(ctx); ok && strings.TrimSpace(toolCtx.WorkDir) != "" {
		dir, err := filepath.Abs(filepath.Clean(strings.TrimSpace(toolCtx.WorkDir)))
		if err != nil {
			return "", fmt.Errorf("resolve tool work dir: %w", err)
		}
		if err := security.PathWithinWorkspace(dir); err != nil {
			return "", err
		}
		return security.CanonicalPath(dir)
	}
	root, err := security.WorkspaceRoot()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Clean(root))
}

func ensureInsideBaseForWrite(ctx context.Context, path string) error {
	base, err := toolWorkDir(ctx)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	ancestor, err := security.NearestExistingAncestor(parent)
	if err != nil {
		return err
	}
	realAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return fmt.Errorf("resolve parent directory symlinks: %w", err)
	}
	return ensureInsideBase(base, realAncestor)
}

func ensureInsideBase(base string, candidate string) error {
	baseAbs, err := security.CanonicalPath(base)
	if err != nil {
		return fmt.Errorf("resolve base path: %w", err)
	}
	candidateAbs, err := security.CanonicalPath(candidate)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	rel, err := filepath.Rel(baseAbs, candidateAbs)
	if err != nil {
		return fmt.Errorf("resolve path relative to work dir: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("path is outside work dir: %s", candidate)
	}
	return nil
}
