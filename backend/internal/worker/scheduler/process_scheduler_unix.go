//go:build !windows

package scheduler

import (
	"os/exec"
	"syscall"
	"time"

	"github.com/ygpkg/yg-go/logs"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func stopProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		logs.Warnf("Failed to get process group for PID %d: %v", cmd.Process.Pid, err)
		return cmd.Process.Kill()
	}

	if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil {
		logs.Warnf("Failed to send SIGINT to process group %d: %v", -pgid, err)
	}

	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		logs.Warnf("Process group %d did not exit gracefully, force killing", -pgid)
		return syscall.Kill(-pgid, syscall.SIGKILL)
	}
}
