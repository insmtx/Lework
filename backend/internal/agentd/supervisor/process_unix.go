//go:build !windows

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// execProcess supervises one child in its own process group. Running the child
// in a new group lets Stop kill helpers it forked (for example the language
// servers opencode launches) instead of orphaning them on the node.
type execProcess struct {
	spec Spec

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan struct{}
	waitErr error
}

// newProcess binds a spec to the platform implementation. The process is not
// started until Start is called, because a supervisor may decide to attach
// instead.
func newProcess(_ context.Context, spec Spec) (Process, error) {
	return &execProcess{spec: spec}, nil
}

// Start launches the command. ctx governs the process lifetime: canceling it
// makes exec kill the child, mirroring Stop.
func (p *execProcess) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.cmd != nil {
		p.mu.Unlock()
		return errors.New("agentd: process already started")
	}

	cmd := exec.CommandContext(ctx, p.spec.Binary, p.spec.Args...)
	// exec's default cancel kills only the direct child; kill the whole group
	// so ctx cancellation cleans up helpers exactly like Stop does.
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	cmd.Env = mergeEnv(os.Environ(), p.spec.Env)
	if p.spec.Dir != "" {
		cmd.Dir = p.spec.Dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("start %s: %w", p.spec.Binary, err)
	}

	p.cmd = cmd
	p.done = make(chan struct{})
	done := p.done
	p.mu.Unlock()

	go func() {
		err := cmd.Wait()

		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(done)
	}()
	return nil
}

// Stop kills the child's whole process group and waits for it to be reaped.
// Killing a group that already exited reports ESRCH, which is not an error.
func (p *execProcess) Stop(ctx context.Context) error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill process group %d: %w", cmd.Process.Pid, err)
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for %s to exit: %w", p.spec.Binary, ctx.Err())
	}
}

// Wait blocks until the child exits and returns the exit error. Callers that
// never started the process get an error instead of a hang.
func (p *execProcess) Wait() error {
	p.mu.Lock()
	done := p.done
	p.mu.Unlock()

	if done == nil {
		return errors.New("agentd: process not started")
	}
	<-done

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}
