package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ygpkg/yg-go/logs"

	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
)

// allOptions 汇总裸 lework 的全部输入，便于测试直接构造。
type allOptions struct {
	serve     controlplaneconfig.Config
	tokenPath string
	explicit  string
	agent     agentOverrides
	pair      pairOptions
}

// runAllCommand 是 runAll 的测试 seam：测试替换它即可验证 root 的接线，
// 无需真正打开存储或启动 OpenCode 子进程。
var runAllCommand = runAll

// runAll 在同一进程内启动控制面与节点 agent，直到信号取消或任一服务失败。
//
// 注册令牌只写进内存配置，日志仅打印 agent 拨打的地址，绝不输出令牌本身。
func runAll(parent context.Context, opts allOptions) error {
	token, err := controlplaneconfig.ResolveToken(opts.explicit, opts.tokenPath)
	if err != nil {
		return fmt.Errorf("resolve enrollment token: %w", err)
	}
	opts.serve.EnrollmentToken = token

	agentCfg, err := resolveAgentConfig(opts.serve, opts.agent)
	if err != nil {
		return err
	}

	cp, err := openControlPlane(opts.serve)
	if err != nil {
		return err
	}

	ar, err := buildAgent(agentCfg)
	if err != nil {
		// 控制面已经打开存储，构造 agent 失败时必须先把资源还回去。
		_ = cp.Close()
		return err
	}

	ctx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	logs.Infof("lework: agent will dial %s", agentCfg.ControlPlaneURL)

	announcePairing(os.Stdout, opts.serve, opts.pair)

	return runServices(ctx, []backendService{
		{name: "controlplane", run: cp.Run, close: cp.Close},
		{name: "agent", run: ar.Run, close: ar.Close},
	})
}
