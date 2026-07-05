package process

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Process is the minimal lifecycle handle exposed by a provider invocation.
type Process interface {
	PID() int
	Stop() error
}

// CmdProcess 实现 os/exec.Cmd 的 Process 接口。
type CmdProcess struct {
	cmd    *exec.Cmd
	done   <-chan struct{}
	mu     sync.Mutex
	closed bool
}

// NewCmdProcess 将 cmd 和调用方拥有的 Wait 完成信号包装为 Process。
// 调用方必须保持 cmd.Wait() 的唯一所有权，并在 Wait 返回后关闭 done。
func NewCmdProcess(cmd *exec.Cmd, done <-chan struct{}) *CmdProcess {
	return &CmdProcess{cmd: cmd, done: done}
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

// gracefulShutdownTimeout 是优雅关闭子进程的等待时间。
const gracefulShutdownTimeout = 5 * time.Second

// Stop 优雅地终止子进程，不调用 cmd.Wait 或 cmd.Process.Wait。
//
// 关闭流程：
//  1. 发送 SIGTERM 请求子进程自行清理
//  2. 等待 gracefulShutdownTimeout
//  3. 超时则强制 SIGKILL
//
// Stop 是幂等的，多次调用安全。
func (p *CmdProcess) Stop() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// 尝试 SIGTERM（优雅关闭）
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		_ = cmd.Process.Kill()
	}

	if done == nil {
		return nil
	}

	// 等待持有 cmd.Wait 所有权的调用方确认退出，超时则强制 Kill。
	select {
	case <-done:
		return nil
	case <-time.After(gracefulShutdownTimeout):
		_ = cmd.Process.Kill()
		<-done
		return nil
	}
}
