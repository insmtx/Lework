package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	appconfig "github.com/insmtx/lework/backend/config"
	agentdconfig "github.com/insmtx/lework/backend/internal/agentd/config"
	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
)

// 统一配置文件让控制面与节点 agent 共用一个 --config：
//
//	controlplane:
//	  listen: 127.0.0.1:8787
//	  db: ~/.config/Lework/controlplane.db
//	  public_url: http://dev.ckeyer.com
//	nats:
//	  url: nats://127.0.0.1:4222
//	database:
//	  url: postgres://lework:lework@127.0.0.1:5432/lework
//	agent:
//	  workspace_roots: [/opt/lework/me/workspace]
//
// 同时兼容旧的扁平 agent.yaml（顶层直接是 control_plane_url 等字段）。
// 加载器只解析、不校验；优先级固定为 flag > 文件 > 派生默认，由调用方保证。

// defaultUnifiedConfigPath 与 agent.yaml 同目录，避免再引入一套路径规则。
func defaultUnifiedConfigPath() string {
	return filepath.Join(filepath.Dir(defaultConfigPath()), "lework.yaml")
}

// resolveConfigPath 选择默认要读的配置文件：优先统一文件，其次旧的
// agent.yaml。返回的路径可能不存在，由调用方决定是否静默忽略。
func resolveConfigPath() string {
	unified := defaultUnifiedConfigPath()
	if _, err := os.Stat(unified); err == nil {
		return unified
	}
	if _, err := os.Stat(defaultConfigPath()); err == nil {
		return defaultConfigPath()
	}
	return unified
}

// controlPlaneFile 是 controlplane 段的文件表示。
//
// 字段用指针区分“没写”与“写了零值”：只有文件里出现过的键才会覆盖默认值。
type controlPlaneFile struct {
	Listen           *string        `yaml:"listen"`
	DB               *string        `yaml:"db"`
	TokenFile        *string        `yaml:"token_file"`
	RequestTimeout   *time.Duration `yaml:"request_timeout"`
	HeartbeatTimeout *time.Duration `yaml:"heartbeat_timeout"`
	PublicURL        *string        `yaml:"public_url"`
	ExpoURL          *string        `yaml:"expo_url"`
}

// unifiedConfig 是 --config 文件的解析结果。
type unifiedConfig struct {
	ControlPlane controlPlaneFile     `yaml:"controlplane"`
	Agent        *agentdconfig.Config `yaml:"agent"`
	// NATS 与 Database 是控制面集群模式的顶层段，与旧 server 配置同名。
	NATS     *appconfig.NATSConfig     `yaml:"nats"`
	Database *appconfig.DatabaseConfig `yaml:"database"`

	// raw 保留展开环境变量后原文，供识别旧的扁平 agent.yaml 使用。
	raw []byte
}

// loadConfigFile 读取配置文件。explicit 为 false 时，默认路径不存在按“没有
// 配置文件”处理；显式指定的路径必须存在，否则报错（拼错路径不该被静默忽略）。
func loadConfigFile(path string, explicit bool) (*unifiedConfig, error) {
	file, err := loadUnifiedConfig(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return &unifiedConfig{}, nil
		}
		return nil, err
	}
	return file, nil
}

func loadUnifiedConfig(path string) (*unifiedConfig, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return &unifiedConfig{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	expanded := []byte(os.ExpandEnv(string(raw)))
	var cfg unifiedConfig
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}
	cfg.raw = expanded
	return &cfg, nil
}

// applyControlPlane 用 controlplane 段补齐未显式传入的 flag。
//
// changed 报告某个 flag 是否由命令行给出；文件永远不覆盖显式 flag，
// 这与 resolveAgentConfig 的优先级一致。
func (c *unifiedConfig) applyControlPlane(
	cfg *controlplaneconfig.Config,
	tokenFile *string,
	pair *pairOptions,
	changed func(string) bool,
) {
	if c == nil || cfg == nil {
		return
	}
	if changed == nil {
		changed = func(string) bool { return false }
	}
	cp := c.ControlPlane
	if cp.Listen != nil && !changed("listen") {
		cfg.ListenAddr = expandHomePath(*cp.Listen)
	}
	if cp.DB != nil && !changed("db") {
		cfg.DBPath = expandHomePath(*cp.DB)
	}
	c.applyTokenFile(tokenFile, changed)
	if cp.RequestTimeout != nil && !changed("request-timeout") {
		cfg.RequestTimeout = *cp.RequestTimeout
	}
	if cp.HeartbeatTimeout != nil && !changed("heartbeat-timeout") {
		cfg.HeartbeatTimeout = *cp.HeartbeatTimeout
	}
	if c.NATS != nil && !changed("nats-url") {
		cfg.NATS = &appconfig.NATSConfig{URL: strings.TrimSpace(c.NATS.URL)}
	}
	if c.Database != nil && !changed("database-url") {
		cfg.Database = &appconfig.DatabaseConfig{
			URL:   strings.TrimSpace(c.Database.URL),
			Debug: c.Database.Debug,
		}
	}
	if pair != nil {
		if cp.PublicURL != nil && !changed("public-url") {
			pair.publicURL = strings.TrimSpace(*cp.PublicURL)
		}
		if cp.ExpoURL != nil && !changed("expo-url") {
			pair.expoURL = strings.TrimSpace(*cp.ExpoURL)
		}
	}
}

// applyClusterFlags 把显式传入的 --nats-url / --database-url 写回配置。
//
// 只覆盖 URL 字段，保留文件里的其他字段（例如 database.debug）；
// 显式传空值会把空 URL 交给 ValidateCluster，而不是静默回退到文件。
func applyClusterFlags(
	cfg *controlplaneconfig.Config,
	natsURL, databaseURL string,
	changed func(string) bool,
) {
	if cfg == nil {
		return
	}
	if changed == nil {
		changed = func(string) bool { return false }
	}
	if changed("nats-url") {
		if cfg.NATS == nil {
			cfg.NATS = &appconfig.NATSConfig{}
		}
		cfg.NATS.URL = strings.TrimSpace(natsURL)
	}
	if changed("database-url") {
		if cfg.Database == nil {
			cfg.Database = &appconfig.DatabaseConfig{}
		}
		cfg.Database.URL = strings.TrimSpace(databaseURL)
	}
}

// applyTokenFile 用文件里的 token_file 补齐未显式传入的 flag。
func (c *unifiedConfig) applyTokenFile(tokenFile *string, changed func(string) bool) {
	if c == nil || tokenFile == nil || c.ControlPlane.TokenFile == nil {
		return
	}
	if changed != nil && changed("token-file") {
		return
	}
	*tokenFile = expandHomePath(*c.ControlPlane.TokenFile)
}

// agentBase 返回文件里的 agent 配置：优先 agent: 段，其次把整个文件当作
// 旧的扁平 agent.yaml 解析。
//
// 一个只写了 controlplane: 的文件不算 agent 配置（返回 nil），调用方应回退到
// 默认 agent.yaml 或派生默认值，避免把控制面配置误读成空 agent 配置。
func (c *unifiedConfig) agentBase() (*agentdconfig.Config, error) {
	if c == nil || len(c.raw) == 0 {
		return nil, nil
	}
	if c.Agent != nil {
		cfg := *c.Agent
		cfg.WorkspaceRoots = append([]string(nil), cfg.WorkspaceRoots...)
		cfg.ApplyDefaults()
		return &cfg, nil
	}
	if !c.looksLikeAgentFile() {
		return nil, nil
	}
	var flat agentdconfig.Config
	if err := yaml.Unmarshal(c.raw, &flat); err != nil {
		return nil, fmt.Errorf("decode agent config: %w", err)
	}
	flat.ApplyDefaults()
	return &flat, nil
}

// looksLikeAgentFile 判断文件是不是没有 agent: 段的旧 agent.yaml。
func (c *unifiedConfig) looksLikeAgentFile() bool {
	var top map[string]any
	if err := yaml.Unmarshal(c.raw, &top); err != nil {
		return false
	}
	if _, ok := top["controlplane"]; ok {
		return false
	}
	for _, key := range []string{
		"control_plane_url", "enrollment_token", "node_name", "workspace_roots",
		"tls_fingerprint", "opencode", "worktree_root", "repo_cache_root",
	} {
		if _, ok := top[key]; ok {
			return true
		}
	}
	return false
}

// loadAgentConfig 读取节点 agent 配置：统一文件的 agent: 段，或旧的扁平
// agent.yaml。文件存在但没有 agent 字段时报错，避免静默启动一个空配置。
func loadAgentConfig(path string) (*agentdconfig.Config, error) {
	file, err := loadUnifiedConfig(path)
	if err != nil {
		return nil, err
	}
	base, err := file.agentBase()
	if err != nil {
		return nil, err
	}
	if base == nil {
		return nil, fmt.Errorf("config %s has no agent section", path)
	}
	return base, nil
}

// expandHomePath 展开路径开头的 ~。配置里写绝对路径最稳，但 ~ 更符合直觉。
func expandHomePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed != "~" && !strings.HasPrefix(trimmed, "~/") {
		return trimmed
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return trimmed
	}
	if trimmed == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(trimmed, "~/"))
}
