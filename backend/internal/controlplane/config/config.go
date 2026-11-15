// Package config 定义控制面本地服务的启动配置。
//
// 控制面是面向单用户的本地服务，默认监听回环地址；如需绑定非回环地址，
// 必须自行配合浏览器鉴权，避免把节点注册与管理接口暴露到局域网或公网。
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	appconfig "github.com/insmtx/lework/backend/config"
)

const (
	// DefaultListenAddr 是控制面的默认监听地址，只绑定本机回环。
	DefaultListenAddr = "127.0.0.1:8787"

	// DefaultRequestTimeout 是控制面处理单次节点请求的默认超时。
	DefaultRequestTimeout = 30 * time.Second

	// DefaultHeartbeatTimeout 是判定节点离线的默认超时，略大于心跳周期。
	DefaultHeartbeatTimeout = 35 * time.Second

	// DefaultSessionTTL 是浏览器会话的默认有效期；个人长期使用下 30 天可避免频繁登录。
	DefaultSessionTTL = 30 * 24 * time.Hour

	// DefaultClaimTTL 是节点认领码的默认有效期，短到足以限制口令被转述的风险。
	DefaultClaimTTL = 5 * time.Minute

	// DefaultClaimCodeLength 是节点认领码的默认长度。
	DefaultClaimCodeLength = 5

	// DefaultRegistrationMode 是默认注册策略：只允许持邀请码的人注册。
	DefaultRegistrationMode = "invite"

	// minClaimCodeLength / maxClaimCodeLength 限定认领码长度：
	// 太短容易被猜中，太长则难以在节点侧手输。
	minClaimCodeLength = 4
	maxClaimCodeLength = 8

	dbFileName    = "controlplane.db"
	configDirName = "Lework"
)

// Config 是控制面本地服务的配置。
type Config struct {
	// ListenAddr 是 HTTP 监听地址，格式为 host:port；默认建议回环，非回环需配合鉴权。
	ListenAddr string
	// DBPath 仅用于测试与向后兼容的配置结构：生产运行路径使用
	// PostgreSQL（见 Database），控制面启动后不会再读取这个字段。
	DBPath string
	// EnrollmentToken 是节点注册时出示的令牌，属于机密信息，不可写入日志或错误。
	EnrollmentToken string
	// RequestTimeout 是处理单次节点请求的超时。
	RequestTimeout time.Duration
	// HeartbeatTimeout 是节点心跳失效的判定超时。
	HeartbeatTimeout time.Duration
	// RegistrationMode 决定谁可以创建账号：invite（默认，凭邀请码）/ open / closed。
	RegistrationMode string
	// SessionTTL 是浏览器会话的有效期。
	SessionTTL time.Duration
	// ClaimTTL 是节点认领码的有效期。
	ClaimTTL time.Duration
	// ClaimCodeLength 是节点认领码的长度，取值 4..8。
	ClaimCodeLength int
	// LocalAdmin 表示本机回环请求无需登录即视为管理员，默认开启。
	LocalAdmin bool
	// NATS 是控制面集群模式的 NATS 连接配置；单机模式保持 nil。
	NATS *appconfig.NATSConfig
	// Database 是控制面集群模式共享的 PostgreSQL 连接配置；单机模式保持 nil。
	Database *appconfig.DatabaseConfig
}

// Default 返回内置默认配置。EnrollmentToken 没有默认值，必须由使用者显式提供。
func Default() Config {
	return Config{
		ListenAddr:       DefaultListenAddr,
		DBPath:           DefaultDBPath(),
		RequestTimeout:   DefaultRequestTimeout,
		HeartbeatTimeout: DefaultHeartbeatTimeout,
		RegistrationMode: DefaultRegistrationMode,
		SessionTTL:       DefaultSessionTTL,
		ClaimTTL:         DefaultClaimTTL,
		ClaimCodeLength:  DefaultClaimCodeLength,
		LocalAdmin:       true,
	}
}

// DefaultDBPath 返回用户配置目录下的默认数据库路径。
// 取不到用户配置目录时退回当前工作目录下的文件名，保证服务仍可启动。
func DefaultDBPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return dbFileName
	}
	return filepath.Join(dir, configDirName, dbFileName)
}

// Validate 校验配置是否可用于启动控制面。
// 错误信息只描述字段与原因，不包含令牌内容，避免机密泄漏到日志。
func (c Config) Validate() error {
	if strings.TrimSpace(c.EnrollmentToken) == "" {
		return errors.New("enrollment_token is required")
	}
	// 令牌会被直接写进 Cookie 值与 Bearer 头，字符集必须事先收口，
	// 否则直接构造的配置会得到静默失效的 Cookie。
	if err := validateTokenCharset(c.EnrollmentToken); err != nil {
		return err
	}

	// 只校验 host:port 格式；是否绑定非回环地址由调用方与鉴权配置决定。
	// SplitHostPort 不校验端口是否为数字，所以还要再过一道 strconv 确认。
	_, port, err := net.SplitHostPort(c.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen_addr %q is invalid: expected host:port: %w", c.ListenAddr, err)
	}
	if n, convErr := strconv.Atoi(port); convErr != nil || n < 0 || n > 65535 {
		return fmt.Errorf("listen_addr %q is invalid: expected host:port, bad port %q", c.ListenAddr, port)
	}

	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be positive, got %s", c.RequestTimeout)
	}
	if c.HeartbeatTimeout <= 0 {
		return fmt.Errorf("heartbeat_timeout must be positive, got %s", c.HeartbeatTimeout)
	}

	switch c.RegistrationMode {
	case "invite", "open", "closed":
	default:
		return fmt.Errorf("registration_mode %q is invalid: want invite, open or closed", c.RegistrationMode)
	}

	if c.SessionTTL <= 0 {
		return fmt.Errorf("session_ttl must be positive, got %s", c.SessionTTL)
	}
	if c.ClaimTTL <= 0 {
		return fmt.Errorf("claim_ttl must be positive, got %s", c.ClaimTTL)
	}
	if c.ClaimCodeLength < minClaimCodeLength || c.ClaimCodeLength > maxClaimCodeLength {
		return fmt.Errorf("claim_code_length must be between %d and %d, got %d",
			minClaimCodeLength, maxClaimCodeLength, c.ClaimCodeLength)
	}

	return nil
}

// ValidateCluster 校验控制面集群依赖的 NATS 与 PostgreSQL 配置。
//
// 只有集群模式才需要这两个地址，因此 Validate 保持只校验本地启动参数；
// 进程入口应在真正打开连接前调用本方法。
//
// 错误文本只描述字段与原因，绝不拼接 URL：URL 可能内嵌账号密码。
func (c Config) ValidateCluster() error {
	if c.NATS == nil || strings.TrimSpace(c.NATS.URL) == "" {
		return errors.New("nats url is required")
	}
	if err := validateNATSScheme(c.NATS.URL); err != nil {
		return err
	}
	if c.Database == nil || strings.TrimSpace(c.Database.URL) == "" {
		return errors.New("database url is required")
	}
	return validateDatabaseScheme(c.Database.URL)
}

// validateNATSScheme 只接受 nats://，避免把 HTTP 地址误当成 NATS。
//
// url.Parse 的错误会带上原始 URL，因此这里不包装它，只返回固定文本。
func validateNATSScheme(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "nats") {
		return errors.New("nats url must use the nats scheme")
	}
	return nil
}

// validateDatabaseScheme 接受 postgres:// 与 postgresql:// 两种写法。
func validateDatabaseScheme(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return errors.New("database url must use the postgres scheme")
	}
	switch strings.ToLower(u.Scheme) {
	case "postgres", "postgresql":
		return nil
	default:
		return errors.New("database url must use the postgres scheme")
	}
}

// IsLoopbackAddr 判断监听地址是否只对本机可见；用于决定是否需要浏览器鉴权告警。
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return isLoopbackHost(host)
}

// DialAddrFor 把监听地址换算成进程内客户端可拨打的地址：
// 空主机名与 0.0.0.0/:: 映射到回环，其余原样返回；解析失败时原样返回。
func DialAddrFor(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "":
		return net.JoinHostPort("127.0.0.1", port)
	case "0.0.0.0":
		return net.JoinHostPort("127.0.0.1", port)
	case "::":
		return net.JoinHostPort("::1", port)
	default:
		return addr
	}
}

// isLoopbackHost 判断主机名是否指向本机回环。
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
