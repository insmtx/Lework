package process

import (
	"errors"
	"os/exec"
	"sync"
	"testing"
)

func TestCmdProcessStopUsesOwnerWait(t *testing.T) {
	cmd := exec.Command("sh", "-c", `trap 'exit 0' TERM; while :; do sleep 1; done`)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}

	done := make(chan struct{})
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
		close(done)
	}()

	process := NewCmdProcess(cmd, done)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := process.Stop(); err != nil {
				t.Errorf("stop process: %v", err)
			}
		}()
	}
	wg.Wait()

	if err := <-waitErr; err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("owner cmd.Wait lost process ownership: %v", err)
		}
	}
}
