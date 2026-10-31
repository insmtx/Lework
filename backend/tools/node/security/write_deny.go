package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var writeSafeRootConfig string

var writeDeniedPaths = map[string]struct{}{
	filepath.Join(os.Getenv("HOME"), ".ssh", "authorized_keys"): {},
	filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa"):          {},
	filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519"):      {},
	filepath.Join(os.Getenv("HOME"), ".ssh", "config"):          {},
	filepath.Join(os.Getenv("HOME"), ".bashrc"):                 {},
	filepath.Join(os.Getenv("HOME"), ".zshrc"):                  {},
	filepath.Join(os.Getenv("HOME"), ".profile"):                {},
	filepath.Join(os.Getenv("HOME"), ".bash_profile"):           {},
	filepath.Join(os.Getenv("HOME"), ".zprofile"):               {},
	filepath.Join(os.Getenv("HOME"), ".netrc"):                  {},
	filepath.Join(os.Getenv("HOME"), ".pgpass"):                 {},
	filepath.Join(os.Getenv("HOME"), ".npmrc"):                  {},
	filepath.Join(os.Getenv("HOME"), ".pypirc"):                 {},
	"/etc/sudoers": {},
	"/etc/passwd":  {},
	"/etc/shadow":  {},
}

var writeDeniedPrefixes = []string{
	filepath.Join(os.Getenv("HOME"), ".ssh"),
	filepath.Join(os.Getenv("HOME"), ".aws"),
	filepath.Join(os.Getenv("HOME"), ".gnupg"),
	filepath.Join(os.Getenv("HOME"), ".kube"),
	filepath.Join(os.Getenv("HOME"), ".docker"),
	filepath.Join(os.Getenv("HOME"), ".azure"),
	filepath.Join(os.Getenv("HOME"), ".config", "gh"),
	"/etc/sudoers.d",
	"/etc/systemd",
}

// SetWriteSafeRoot 设置安全写入根目录
func SetWriteSafeRoot(root string) {
	writeSafeRootConfig = root
}

// GetSafeWriteRoot 获取安全写入根目录，优先使用配置值，其次使用环境变量
func GetSafeWriteRoot() string {
	if writeSafeRootConfig != "" {
		realRoot, err := filepath.EvalSymlinks(filepath.Clean(writeSafeRootConfig))
		if err != nil {
			return ""
		}
		return realRoot
	}
	root := os.Getenv("LEWORK_WRITE_SAFE_ROOT")
	if root == "" {
		return ""
	}
	realRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return ""
	}
	return realRoot
}

// IsWriteDenied 检查路径是否被拒绝写入（黑名单检查）
func IsWriteDenied(path string) error {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("resolve path symlinks: %w", err)
		}
		resolved, err = filepath.Abs(filepath.Clean(path))
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}
	}

	if safeRoot := GetSafeWriteRoot(); safeRoot != "" {
		if resolved != safeRoot && !strings.HasPrefix(resolved, safeRoot+string(filepath.Separator)) {
			return fmt.Errorf("write denied: path is outside safe root %s", os.Getenv("LEWORK_WRITE_SAFE_ROOT"))
		}
	}

	if _, denied := writeDeniedPaths[resolved]; denied {
		return fmt.Errorf("write denied: %s is a protected path", path)
	}

	for _, prefix := range writeDeniedPrefixes {
		if strings.HasPrefix(resolved, prefix+string(filepath.Separator)) {
			return fmt.Errorf("write denied: path is in protected directory %s", prefix)
		}
	}

	return nil
}
