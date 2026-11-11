// Package config 定义控制面本地服务的启动配置。
//
// 控制面是面向单用户的本地服务，默认监听回环地址；如需绑定非回环地址，
// 必须自行配合浏览器鉴权，避免把节点注册与管理接口暴露到局域网或公网。
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultListenAddr 是控制面的默认监听地址，只绑定本机回环。
	DefaultListenAddr = "127.0.0.1:8787"

	// DefaultRequestTimeout 是控制面处理单次节点请求的默认超时。
	DefaultRequestTimeout = 30 * time.Second

	// DefaultHeartbeatTimeout 是判定节点离线的默认超时，略大于心跳周期。
	DefaultHeartbeatTimeout = 35 * time.Second

	dbFileName    = "controlplane.db"
	configDirName = "Lework"
)

// Config 是控制面本地服务的配置。
type Config struct {
	// ListenAddr 是 HTTP 监听地址，格式为 host:port；默认建议回环，非回环需配合鉴权。
	ListenAddr string
	// DBPath 是控制面 SQLite 数据库文件路径。
	DBPath string
	// EnrollmentToken 是节点注册时出示的令牌，属于机密信息，不可写入日志或错误。
	EnrollmentToken string
	// RequestTimeout 是处理单次节点请求的超时。
	RequestTimeout time.Duration
	// HeartbeatTimeout 是节点心跳失效的判定超时。
	HeartbeatTimeout time.Duration
}

// Default 返回内置默认配置。EnrollmentToken 没有默认值，必须由使用者显式提供。
func Default() Config {
	return Config{
		ListenAddr:       DefaultListenAddr,
		DBPath:           DefaultDBPath(),
		RequestTimeout:   DefaultRequestTimeout,
		HeartbeatTimeout: DefaultHeartbeatTimeout,
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

	return nil
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
