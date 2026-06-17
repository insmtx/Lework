package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

// WorkerMountedWorkspacePath 返回 server 视角下某个 worker 的 workspace 挂载目录。
// Server 挂载的是 workspace 根目录，单个 Worker 的实际目录位于
// {workspaceRoot}/{orgID}/{workerID}/workspace。
func WorkerMountedWorkspacePath(orgID uint, workerID uint) (string, error) {
	if orgID == 0 {
		return "", fmt.Errorf("org_id is required")
	}
	if workerID == 0 {
		return "", fmt.Errorf("worker_id is required")
	}
	root, err := leros.WorkspaceRoot()
	if err != nil {
		return "", err
	}
	workspacePath := filepath.Join(root, fmt.Sprintf("%d", orgID), fmt.Sprintf("%d", workerID), "workspace")
	return filepath.Abs(workspacePath)
}

// ProjectRepoPath 返回项目 repo 在 worker workspace 下的绝对路径。
// 路径格式：{workspaceRoot}/{orgID}/{workerID}/workspace/projects/{orgID}/{publicID}/repo
func ProjectRepoPath(orgID uint, workerID uint, publicID string) (string, error) {
	workspacePath, err := WorkerMountedWorkspacePath(orgID, workerID)
	if err != nil {
		return "", fmt.Errorf("resolve worker workspace: %w", err)
	}
	if strings.TrimSpace(publicID) == "" {
		return "", fmt.Errorf("public_id is required")
	}
	return filepath.Join(workspacePath, "projects", fmt.Sprintf("%d", orgID), publicID, "repo"), nil
}

// FindRepoRoot 从给定的工作目录向上查找包含 .leros 目录的 repo 根目录。
func FindRepoRoot(workDir string) (string, error) {
	dir := filepath.Clean(workDir)
	for {
		lerosDir := filepath.Join(dir, ".leros")
		if info, err := os.Stat(lerosDir); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root not found from %s", workDir)
		}
		dir = parent
	}
}

// ProjectMemoryPath 返回项目记忆文件的绝对路径。
// repoDir 是项目 repo 根目录的绝对路径。
func ProjectMemoryPath(repoDir string) string {
	return filepath.Join(repoDir, ".leros", "memory", "project_memory.md")
}

// ArtifactStoragePath 从 server 侧解析 worker workspace 相对的 storage key。
func ArtifactStoragePath(orgID uint, workerID uint, storageKey string) (string, error) {
	key := strings.TrimSpace(storageKey)
	if key == "" || filepath.IsAbs(key) {
		return "", fmt.Errorf("invalid artifact storage key")
	}
	workspacePath, err := WorkerMountedWorkspacePath(orgID, workerID)
	if err != nil {
		return "", err
	}
	workspaceAbs, err := filepath.Abs(workspacePath)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(filepath.Join(workspaceAbs, filepath.FromSlash(key)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(workspaceAbs, pathAbs)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("artifact storage key escapes workspace")
	}
	return pathAbs, nil
}
