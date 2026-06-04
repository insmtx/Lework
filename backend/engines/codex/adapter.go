// Package codex 将 Codex CLI 适配到 Leros 外部 CLI 引擎接口。
// 使用 codex app-server --listen stdio:// 模式进行通信。
package codex

import (
	"context"
	"os"
	"path/filepath"

	"github.com/insmtx/Leros/backend/engines"
)

// Adapter 通过 Codex CLI app-server 模式执行提示。
type Adapter struct {
	invoker *AppServerInvoker
}

// NewAdapter 创建 Codex CLI 引擎适配器（app-server 模式）。
func NewAdapter(binary string, extraEnv map[string]string) *Adapter {
	if binary == "" {
		binary = "codex"
	}
	return &Adapter{invoker: NewAppServerInvoker(binary, extraEnv)}
}

// Prepare 执行 Codex 工作区设置（当前为空实现）。
func (a *Adapter) Prepare(_ context.Context, _ engines.PrepareRequest) error {
	return nil
}

// RegisterMCP registers a streamable HTTP MCP server with Codex CLI.
func (a *Adapter) RegisterMCP(ctx context.Context, cfg engines.MCPServerConfig) error {
	cfg = engines.NormalizeMCPServerConfig(cfg)
	_ = engines.RunCLICommand(ctx, a.invoker.binary, []string{"mcp", "remove", cfg.Name}, nil)

	args := []string{"mcp", "add", cfg.Name, "--url", cfg.URL}
	env := []string(nil)
	if cfg.BearerToken != "" {
		tokenEnvVar := engines.LerosMCPTokenEnvVar()
		args = append(args, "--bearer-token-env-var", tokenEnvVar)
		env = append(env, tokenEnvVar+"="+cfg.BearerToken)
	}
	return engines.RunCLICommand(ctx, a.invoker.binary, args, env)
}

// Run 启动 Codex CLI 并返回进程句柄。
func (a *Adapter) Run(ctx context.Context, req engines.RunRequest) (*engines.RunHandle, error) {
	handle, err := a.invoker.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	return handle, nil
}

// GetSkillDir returns the skill directory path for Codex CLI.
func (a *Adapter) GetSkillDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", "skills")
}

var _ engines.Engine = (*Adapter)(nil)
