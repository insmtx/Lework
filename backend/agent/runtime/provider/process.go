package provider

import (
	"os/exec"
	"sync"
)

// Process is the minimal lifecycle handle exposed by a provider invocation.
type Process interface {
	PID() int
	Stop() error
}

// CmdProcess 实现 os/exec.Cmd 的 Process 接口。
type CmdProcess struct {
	cmd *exec.Cmd
	mu  sync.Mutex
}

// NewCmdProcess 将 cmd 包装为 Process。
func NewCmdProcess(cmd *exec.Cmd) *CmdProcess {
	return &CmdProcess{cmd: cmd}
}

// PID 返回命令启动后的进程 ID。
func (p *CmdProcess) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Stop 如果进程仍在运行则终止它。
func (p *CmdProcess) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}
