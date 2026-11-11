package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	agentdconfig "github.com/insmtx/lework/backend/internal/agentd/config"
	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
)

// userHomeDir、hostname 与 defaultAgentConfigPath 是测试 seam，
// 生产环境分别使用 os 与平台默认路径的实现。
var (
	userHomeDir            = os.UserHomeDir
	hostname               = os.Hostname
	defaultAgentConfigPath = defaultConfigPath
)

// derivedNodeNameFallback 在取不到主机名时使用，保证组合模式仍能启动。
const derivedNodeNameFallback = "lework-local"

// agentOverrides 是命令行显式指定、优先级高于 agent.yaml 的字段。
//
// configPathExplicit 区分“用户显式传了 --agent-config”与“使用默认路径”：
// 前者指向不存在的文件必须报错，后者缺失时静默回退到派生默认值。
type agentOverrides struct {
	configPath         string
	configPathExplicit bool
	nodeName           string
	workspaceRoots     []string
}

// resolveAgentConfig 按 k3s 风格的优先级生成节点 agent 配置：
// 命令行覆盖 > 显式 --agent-config > 默认路径的 agent.yaml（若存在）> 派生默认值。
//
// 派生阶段只补空字段，文件里显式写出的 control_plane_url 与 enrollment_token
// 会被原样保留。这是有意的产品决策：组合模式不覆盖用户已经配置好的地址与令牌。
func resolveAgentConfig(serveCfg controlplaneconfig.Config, ov agentOverrides) (*agentdconfig.Config, error) {
	cfg, err := loadBaseAgentConfig(ov)
	if err != nil {
		return nil, err
	}

	// 只在需要派生的分支检查端口，避免干扰显式配置了 URL 的场景。
	if cfg.ControlPlaneURL == "" {
		url, err := deriveControlPlaneURL(serveCfg.ListenAddr)
		if err != nil {
			return nil, err
		}
		cfg.ControlPlaneURL = url
	}
	if cfg.EnrollmentToken == "" {
		cfg.EnrollmentToken = serveCfg.EnrollmentToken
	}
	if cfg.NodeName == "" {
		cfg.NodeName = resolveNodeName()
	}
	if len(cfg.WorkspaceRoots) == 0 {
		root, err := userHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine default workspace root: %w (pass --workspace-root to set it explicitly)", err)
		}
		cfg.WorkspaceRoots = []string{root}
	}

	if ov.nodeName != "" {
		cfg.NodeName = ov.nodeName
	}
	if len(ov.workspaceRoots) > 0 {
		cfg.WorkspaceRoots = append([]string(nil), ov.workspaceRoots...)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("agent config: %w", err)
	}
	return cfg, nil
}

// loadBaseAgentConfig 读取配置来源：显式路径必须成功，默认路径存在才读取，
// 都不满足时返回内置默认值。OpenCode 的默认值始终来自 agentdconfig，避免
// 在组合层复制一份。
func loadBaseAgentConfig(ov agentOverrides) (*agentdconfig.Config, error) {
	if ov.configPathExplicit {
		if ov.configPath == "" {
			return nil, errors.New("agent config path is empty")
		}
		return loadAgentConfigFile(ov.configPath)
	}

	path := ov.configPath
	if path == "" {
		path = defaultAgentConfigPath()
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := agentdconfig.Default()
			return &cfg, nil
		}
		return nil, fmt.Errorf("stat agent config %s: %w", path, err)
	}
	return loadAgentConfigFile(path)
}

// loadAgentConfigFile 读取 agent.yaml 并补充 agentd 内置默认值。
func loadAgentConfigFile(path string) (*agentdconfig.Config, error) {
	cfg, err := agentdconfig.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load agent config: %w", err)
	}
	return cfg, nil
}

// deriveControlPlaneURL 把控制面的监听地址换算成节点 agent 拨打的 WSS 地址。
//
// 组合模式要求监听端口固定：端口为 0 时控制面会随机分配端口，agent 无法
// 通过派生地址稳定连回自身，因此直接拒绝而不是生成一个连不上的 URL。
func deriveControlPlaneURL(listenAddr string) (string, error) {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", fmt.Errorf("derive control plane url from listen address %q: %w", listenAddr, err)
	}
	if port == "0" {
		return "", fmt.Errorf("--listen port must be fixed in combined mode, got %q", listenAddr)
	}
	return "ws://" + controlplaneconfig.DialAddrFor(listenAddr) + "/api/agent/connect", nil
}

// resolveNodeName 返回本机主机名，取不到时退回固定名称。
func resolveNodeName() string {
	name, err := hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return derivedNodeNameFallback
	}
	return name
}
