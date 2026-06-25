// Package opencode 将 OpenCode CLI 适配到 Leros 外部 CLI 引擎接口。
// 使用 opencode serve 模式，通过 HTTP REST API + SSE 进行通信。
package opencode

import (
	"context"
	"os"
	"path/filepath"

	"github.com/insmtx/Leros/backend/engines"
)

// Adapter 通过 OpenCode CLI serve 模式执行提示。
type Adapter struct {
	invoker *ServerInvoker
}

// NewAdapter 创建 OpenCode CLI 引擎适配器（serve 模式）。
func NewAdapter(binary string, extraEnv map[string]string) *Adapter {
	if binary == "" {
		binary = "opencode"
	}
	return &Adapter{invoker: NewServerInvoker(binary, extraEnv)}
}

// Prepare 执行 OpenCode 工作区设置（当前为空实现）。
func (a *Adapter) Prepare(_ context.Context, _ engines.PrepareRequest) error {
	return nil
}

// Run 启动 OpenCode serve 并返回进程句柄。
func (a *Adapter) Run(ctx context.Context, req engines.RunRequest) (*engines.RunHandle, error) {
	handle, err := a.invoker.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	return handle, nil
}

// GetSkillDir 返回 OpenCode CLI 的技能目录路径。
func (a *Adapter) GetSkillDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "opencode", "skills")
}

var _ engines.Engine = (*Adapter)(nil)
