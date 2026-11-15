// Package authn 提供控制面账号体系所需的最底层凭证工具：密码哈希、会话令牌
// 与配对短码。
//
// 这些函数只做纯粹的编码与校验，不接触数据库、日志或 HTTP，因此可以被登录、
// 邀请与节点认领等上层流程复用。凭证、明文密码与短码属于机密信息，本包从不
// 打印或记录它们，调用方也必须保证不把它们写进日志或错误文本。
package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id 的成本参数。写死在生成结果里，升级参数不会让旧哈希失效：
// 校验时以 PHC 字符串里的值为准。
const (
	argon2Version = 19
	argon2Time    = 3
	argon2Memory  = 64 * 1024
	argon2Threads = 2

	saltLength = 16
	keyLength  = 32
)

// 解析出来的参数直接决定内存与 CPU 开销，必须夹在合理区间内：伪造的 PHC
// 字符串若声称 m=4194304 就会让 argon2 一次分配数 GiB 内存。
// argon2 在 time < 1 或 threads < 1 时会 panic，这里一并挡掉。
const (
	minArgon2Memory  = 8
	maxArgon2Memory  = 1 << 20 // 1 GiB
	maxArgon2Time    = 16
	maxArgon2Threads = 16
	minSaltLength    = 8
	maxSaltLength    = 64
	minKeyLength     = 16
	maxKeyLength     = 64
)

// HashPassword 用 argon2id 把明文密码编码成 PHC 字符串。
//
// 每次调用都取新的随机 salt，所以同一密码两次哈希结果不同，调用方不能依赖
// 可比较性；需要判断密码是否相同请调用 VerifyPassword。
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码 salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, keyLength)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2Version, argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword 以恒定时间比较 password 与 encoded 是否匹配。
//
// 解析失败一律返回 false 而不是 error：调用方拿不到"哈希格式坏了"与"密码错"
// 的区别，避免由此推断账号是否存在或哈希参数是否被改写。
func VerifyPassword(encoded, password string) bool {
	params, salt, want, ok := parsePHC(encoded)
	if !ok {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads,
		uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// argon2Params 是 PHC 字符串中随哈希一起存档的成本参数。
type argon2Params struct {
	memory  uint32
	time    uint32
	threads uint8
}

// parsePHC 拆分 argon2id$v=19$m=...,t=...,p=...$salt$hash。
//
// 返回 false 表示字符串不符合预期格式或参数越界，调用方据此直接判定校验失败。
func parsePHC(encoded string) (argon2Params, []byte, []byte, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return argon2Params{}, nil, nil, false
	}
	if parts[1] != "v="+strconv.Itoa(argon2Version) {
		return argon2Params{}, nil, nil, false
	}
	params, ok := parseArgon2Params(parts[2])
	if !ok {
		return argon2Params{}, nil, nil, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < minSaltLength || len(salt) > maxSaltLength {
		return argon2Params{}, nil, nil, false
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(key) < minKeyLength || len(key) > maxKeyLength {
		return argon2Params{}, nil, nil, false
	}
	return params, salt, key, true
}

// parseArgon2Params 解析固定的 m,t,p 顺序，并做区间检查。
func parseArgon2Params(s string) (argon2Params, bool) {
	fields := strings.Split(s, ",")
	if len(fields) != 3 {
		return argon2Params{}, false
	}
	memory, ok := parseParam(fields[0], "m", minArgon2Memory, maxArgon2Memory)
	if !ok {
		return argon2Params{}, false
	}
	time, ok := parseParam(fields[1], "t", 1, maxArgon2Time)
	if !ok {
		return argon2Params{}, false
	}
	threads, ok := parseParam(fields[2], "p", 1, maxArgon2Threads)
	if !ok {
		return argon2Params{}, false
	}
	return argon2Params{memory: uint32(memory), time: uint32(time), threads: uint8(threads)}, true
}

// parseParam 读取 name=value 并把 value 限制在 [lower,upper] 内。
func parseParam(field, name string, lower, upper uint64) (uint64, bool) {
	value, found := strings.CutPrefix(field, name+"=")
	if !found {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || n < lower || n > upper {
		return 0, false
	}
	return n, true
}
