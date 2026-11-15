package authn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// tokenBytes 是会话令牌的随机字节数：256 位熵足以抵御离线穷举。
const tokenBytes = 32

// shortCodeAlphabet 剔除了 0/O/1/I/L 等易混淆字符，因为短码要靠人读出或手输。
const shortCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// 短码长度下限避免撞码，上限避免用户无法手输。
const (
	minShortCodeLength = 4
	maxShortCodeLength = 12
)

// NewToken 生成会话令牌，同时返回明文与可直接入库的哈希。
//
// 控制面只保存哈希：数据库泄露时攻击者拿不到可直接使用的会话令牌。明文只在
// 登录响应里出现一次，调用方不得落日志。
func NewToken() (raw, hash string, err error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("生成会话令牌: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashToken(raw), nil
}

// HashToken 返回令牌的 sha256 十六进制摘要。
//
// 令牌本身已是高熵随机值，不需要加盐或慢哈希；固定摘要让上层可以按哈希建
// 唯一索引查找会话。
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// NewShortCode 生成指定长度的配对短码。
//
// length 限制在 4..12：越界直接报错而不是悄悄夹取，避免调用方以为拿到了
// 自己要求的长度。短码熵有限，上层仍需限制尝试次数与有效期。
func NewShortCode(length int) (string, error) {
	if length < minShortCodeLength || length > maxShortCodeLength {
		return "", fmt.Errorf("短码长度 %d 超出 %d..%d", length, minShortCodeLength, maxShortCodeLength)
	}
	limit := big.NewInt(int64(len(shortCodeAlphabet)))
	var code strings.Builder
	code.Grow(length)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("生成短码: %w", err)
		}
		code.WriteByte(shortCodeAlphabet[n.Int64()])
	}
	return code.String(), nil
}
