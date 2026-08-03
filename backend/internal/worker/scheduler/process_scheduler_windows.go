//go:build windows

package scheduler

import (
	"os"
	"os/exec"
	"time"

	"github.com/ygpkg/yg-go/logs"
)

func configureProcessGroup(cmd *exec.Cmd) {
}

func stopProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		logs.Warnf("Failed to send interrupt to PID %d: %v", cmd.Process.Pid, err)
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
		logs.Warnf("Process %d did not exit gracefully, force killing", cmd.Process.Pid)
		return cmd.Process.Kill()
	}
}
