// Package leros provides shared Leros filesystem conventions.
package leros

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// EnvWorkspaceRoot is the worker-local root used for Leros state.
	EnvWorkspaceRoot = "LEROS_WORKSPACE_ROOT"

	// EnvAuthToken is the JWT bearer token used for worker↔server authentication.
	EnvAuthToken = "LEROS_AUTH_TOKEN"

	stateDirName = ".leros"
	stateDBName  = "leros.db"

	defaultWorkspaceRootUnix = "/workspace"
	defaultWindowsAppName    = "Leros"
	defaultWorkspaceDirName  = "workspace"
)

// WorkspaceRoot returns $LEROS_WORKSPACE_ROOT, or the platform default workspace root when unset.
func WorkspaceRoot() (string, error) {
	root := strings.TrimSpace(os.Getenv(EnvWorkspaceRoot))
	if root == "" {
		var err error
		root, err = defaultWorkspaceRoot(runtime.GOOS)
		if err != nil {
			return "", err
		}
	}

	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", EnvWorkspaceRoot, err)
	}
	return absolute, nil
}

// JoinWorkspace joins path elements under the Leros workspace root.
func JoinWorkspace(elem ...string) (string, error) {
	root, err := WorkspaceRoot()
	if err != nil {
		return "", err
	}
	parts := append([]string{root}, elem...)
	return filepath.Join(parts...), nil
}

// SkillsDir returns the default Leros skills directory under workspace_root/.leros.
func SkillsDir() (string, error) {
	return JoinWorkspace(stateDirName, "skills")
}

// MemoryDir returns the default Leros memory directory under workspace_root/.leros.
func MemoryDir() (string, error) {
	return JoinWorkspace(stateDirName, "memory")
}

// EnsureStateDir 确保 workspace_root/.leros 目录存在并返回其路径。
func EnsureStateDir() (string, error) {
	dir, err := JoinWorkspace(stateDirName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create state dir %s: %w", dir, err)
	}
	return dir, nil
}

// StateDBPath 返回状态数据库文件的完整路径（workspace_root/.leros/leros.db），不保证文件存在。
func StateDBPath() (string, error) {
	return JoinWorkspace(stateDirName, stateDBName)
}

// TempDir returns the fallback workspace directory used when a run has no project workspace.
func TempDir() (string, error) {
	return JoinWorkspace("temp")
}

func defaultWorkspaceRoot(goos string) (string, error) {
	if goos != "windows" {
		return defaultWorkspaceRootUnix, nil
	}

	localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if localAppData == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve Windows local application data: %w", err)
		}
		localAppData = strings.TrimSpace(userCacheDir)
	}
	if localAppData == "" {
		return "", fmt.Errorf("Windows local application data is empty")
	}
	return filepath.Join(localAppData, defaultWindowsAppName, defaultWorkspaceDirName), nil
}
