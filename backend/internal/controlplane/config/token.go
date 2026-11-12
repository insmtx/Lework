package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// tokenFileName 是设备上默认令牌文件的文件名，与 DefaultTokenPath 配套。
	tokenFileName = "enrollment.token"

	// minTokenLength 是可接受令牌的最小长度，过短的文件或显式值一律视为无效。
	minTokenLength = 16

	// tokenBytes 是随机令牌的字节数；base64url raw 编码后固定为 43 个字符。
	tokenBytes = 32
)

// userConfigDir 是测试 seam：生产环境始终使用 os.UserConfigDir。
var userConfigDir = os.UserConfigDir

// DefaultTokenPath 返回设备上默认的注册令牌文件位置。
// 取不到用户配置目录时退回当前工作目录下的文件名，与 DefaultDBPath 的退回策略一致。
func DefaultTokenPath() string {
	dir, err := userConfigDir()
	if err != nil {
		return tokenFileName
	}
	return filepath.Join(dir, configDirName, tokenFileName)
}

// ResolveToken 返回生效的注册令牌，优先级为：显式值 > 文件 > 新生成。
// 显式值会写回文件，保证 `lework serve token` 与实际生效值一致。
// 令牌必须同时能用作 Cookie 值与 Bearer 令牌，否则在写入前就拒绝。
// 错误信息只包含路径与原因，绝不包含令牌内容。
func ResolveToken(explicit, path string) (string, error) {
	if explicit != "" {
		if len(explicit) < minTokenLength {
			return "", fmt.Errorf("explicit token is too short: at least %d characters required", minTokenLength)
		}
		// 先校验字符集再落盘，避免把无法使用的令牌持久化成“看似生效”的配置。
		if err := validateTokenCharset(explicit); err != nil {
			return "", err
		}
		if err := writeTokenFile(path, explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}

	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) < minTokenLength {
			return "", fmt.Errorf("token file %s does not contain a valid token", path)
		}
		if err := validateTokenCharset(token); err != nil {
			return "", fmt.Errorf("token file %s: %w", path, err)
		}
		return token, nil
	}
	// 文件不存在属于正常的零配置场景，落到生成新令牌；其余读取失败要报告。
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read token file %s: %w", path, err)
	}

	token, err := generateToken()
	if err != nil {
		return "", err
	}
	if err := writeTokenFile(path, token); err != nil {
		return "", err
	}
	return token, nil
}

// tokenCharsetError 说明令牌字符集约束；单独成变量以保证错误文案唯一，且不含任何令牌内容。
var tokenCharsetError = errors.New(
	"enrollment token contains characters that cannot be used in a cookie or Bearer header; " +
		"use printable ASCII without spaces, quotes, commas, semicolons or backslashes")

// validateTokenCharset 校验令牌能否原样用于 RFC 6265 Cookie 与 Authorization: Bearer 头。
// 允许 RFC 6265 cookie-octet（0x21..0x7E 去掉双引号、逗号、分号与反斜杠），
// 这样 Cookie 值无需编码即可等于令牌，Bearer 头也能直接携带；
// 错误只描述约束，不回显令牌，避免机密进入日志。
func validateTokenCharset(token string) error {
	for i := 0; i < len(token); i++ {
		if !isTokenByte(token[i]) {
			return tokenCharsetError
		}
	}
	return nil
}

// isTokenByte 判断单个字节是否属于允许的 cookie-octet 集合。
func isTokenByte(b byte) bool {
	if b < 0x21 || b > 0x7E {
		return false
	}
	switch b {
	case '"', ',', ';', '\\':
		return false
	}
	return true
}

// generateToken 用 crypto/rand 生成注册令牌；令牌是机密，只返回值、不写日志。
func generateToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// writeTokenFile 以 0700 目录 + 0600 文件权限原子写入令牌：
// 先写同目录临时文件再 Rename，避免并发或中断留下半截令牌。
func writeTokenFile(path, token string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create token dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".enrollment-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp token file under %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	// Rename 成功后临时文件已不存在，Remove 报错可忽略；失败路径才需要清理。
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.WriteString(token); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp token file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp token file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp token file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename token file to %s: %w", path, err)
	}
	return nil
}
