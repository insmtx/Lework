// Package credential 管理节点凭证文件：控制面在 welcome 里一次性下发的
// 明文凭证落盘到这里，之后每次拨号都从这个文件读出并用它构造握手头。
//
// 文件永远以 0600 写入、以 0700 的目录保存，并且替换走临时文件 + rename，
// 因此进程崩溃不会留下被截断的凭证。凭证明文只经过内存与本包，任何错误都
// 只点名路径，不包含内容。
package credential

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// credentialFileName 是凭证在配置文件同目录下的固定文件名。
const credentialFileName = "node.credential"

// userConfigDir 是 os.UserConfigDir 的测试 seam：测试替换它即可验证默认路径
// 的两种分支，无需依赖运行测试的机器真的没有 home 目录。
var userConfigDir = os.UserConfigDir

// DefaultPath 返回节点凭证的落盘位置。
//
// 显式的 agent.yaml 路径优先：凭证与配置文件同目录，运维只需记住一个目录。
// 没有配置文件时（组合启动模式）使用平台配置目录下的 Lework/node.credential，
// 与设计里的 ~/.config/Lework/node.credential 一致。平台目录不可得时退回
// 相对路径 "node.credential"，让启动至少能继续，而不是凭空猜一个绝对路径。
func DefaultPath(configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		dir, err := userConfigDir()
		if err != nil || dir == "" {
			return credentialFileName
		}
		return filepath.Join(dir, "Lework", credentialFileName)
	}
	return filepath.Join(filepath.Dir(configPath), credentialFileName)
}

// Load 读取凭证文件。
//
// 文件不存在与文件内容为空都表示「还没有凭证」，返回 ("", false, nil)：
// 调用方据此回落到 enrollment token 或配对流程，而不是把空串当成凭证。
// 其它读取错误会被包装并点名路径，路径不是秘密，内容才是。
func Load(path string) (string, bool, error) {
	if strings.TrimSpace(path) == "" {
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read node credential %s: %w", path, err)
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return "", false, nil
	}
	return raw, true, nil
}

// Save 原子写入凭证。
//
// 先写同目录临时文件、再 rename 覆盖目标：rename 在同一文件系统内是原子的，
// 因此进程在任意时刻崩溃都只会留下旧凭证或新凭证，不会留下被截断的文件
// （半截凭证会让节点永久 401）。目录权限 0700、文件权限 0600 保证凭证只有
// 运行 agent 的用户可读。raw 永远不会出现在错误文本里。
func Save(path, raw string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("save node credential: path is empty")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("save node credential: credential is empty")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create node credential directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".node.credential-*")
	if err != nil {
		return fmt.Errorf("create node credential temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// 任何一步失败都要清掉临时文件：残留的 0600 文件本身不是秘密，但堆在
	// 配置目录里会让人误以为它是可用的凭证。
	cleanup := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	// CreateTemp 已经是 0600；显式设置一次以固化契约，不依赖标准库实现细节。
	if err := tmp.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("chmod node credential temp file: %w", err))
	}
	if _, err := tmp.WriteString(raw + "\n"); err != nil {
		return cleanup(fmt.Errorf("write node credential temp file: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync node credential temp file: %w", err))
	}
	if err := tmp.Close(); err != nil {
		return cleanup(fmt.Errorf("close node credential temp file: %w", err))
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace node credential %s: %w", path, err)
	}
	return nil
}
