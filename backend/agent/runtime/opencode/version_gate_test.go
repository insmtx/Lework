package opencode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/agent"
)

// withShortProbeTimings 把探测相关的时限压到秒级以内，避免单测真的等 3s/30s。
func withShortProbeTimings(t *testing.T) {
	t.Helper()
	quick, lazy, wait, delay := quickProbeTimeout, lazyProbeTimeout, executeProbeWait, probeWaitDelay
	quickProbeTimeout = 1 * time.Second
	lazyProbeTimeout = 1 * time.Second
	executeProbeWait = 50 * time.Millisecond
	probeWaitDelay = 100 * time.Millisecond
	t.Cleanup(func() {
		quickProbeTimeout, lazyProbeTimeout = quick, lazy
		executeProbeWait, probeWaitDelay = wait, delay
	})
}

// waitBackgroundProbeEnd 等后台补探测退出，避免 goroutine 活过用例、在临时目录被删后再 exec。
func waitBackgroundProbeEnd(t *testing.T, runtime *Runtime) {
	t.Helper()
	if runtime == nil || runtime.gate == nil || runtime.gate.backgroundDone == nil {
		return
	}
	select {
	case <-runtime.gate.backgroundDone:
	case <-time.After(5 * time.Second):
		t.Fatal("background version probe did not finish")
	}
}

// TestProbeVersionBoundsHungChildPipes 覆盖孙进程继承 stdout 时的管道挂死：
// 只靠 CommandContext 的 kill 不够，必须靠 WaitDelay 给 Wait 封顶。
func TestProbeVersionBoundsHungChildPipes(t *testing.T) {
	withShortProbeTimings(t)

	binary := writeFakeOpencode(t, `( sleep 5 ) & echo "2.1.4"`)

	start := time.Now()
	got, err := ProbeVersion(context.Background(), binary, 3*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ProbeVersion() error = %v, want version despite inherited pipe", err)
	}
	if got.String() != "2.1.4" {
		t.Fatalf("version = %s, want 2.1.4", got)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("probe blocked on inherited pipe for %s, want bounded by WaitDelay", elapsed)
	}
}

// TestNewDegradesOnProbeTimeout 固化“探测超时不再让构造失败”。
func TestNewDegradesOnProbeTimeout(t *testing.T) {
	withShortProbeTimings(t)
	quickProbeTimeout = 300 * time.Millisecond
	lazyProbeTimeout = 300 * time.Millisecond

	binary := writeFakeOpencode(t, `sleep 30`)

	start := time.Now()
	runtime, err := New(binary, agent.RuntimeAdapterOptions{}, t.TempDir())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("New() error = %v, want degraded runtime", err)
	}
	if runtime == nil || runtime.gate == nil {
		t.Fatal("New() returned runtime without version gate")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("New() blocked for %s, want bounded by quickProbeTimeout", elapsed)
	}
	if _, _, resolved := runtime.gate.wait(10 * time.Millisecond); resolved {
		t.Fatal("gate resolved while background probe is still running")
	}
	waitBackgroundProbeEnd(t, runtime)
}

// TestNewUsesCachedVersionWithoutExec 保证第二次启动不再 exec 冷启动很慢的 CLI。
func TestNewUsesCachedVersionWithoutExec(t *testing.T) {
	withShortProbeTimings(t)

	marker := filepath.Join(t.TempDir(), "executed")
	binary := writeFakeOpencode(t, fmt.Sprintf("echo run >> %s\necho \"2.1.4\"", marker))
	dataDir := t.TempDir()

	first, err := New(binary, agent.RuntimeAdapterOptions{}, dataDir)
	if err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	waitBackgroundProbeEnd(t, first)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("first New() should execute the probe: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove marker: %v", err)
	}

	second, err := New(binary, agent.RuntimeAdapterOptions{}, dataDir)
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	waitBackgroundProbeEnd(t, second)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second New() must reuse probe cache, marker stat err = %v", err)
	}
}

// TestVersionGateWarnModeAllowsIncompatible 覆盖应急开关：版本不符只告警。
func TestVersionGateWarnModeAllowsIncompatible(t *testing.T) {
	withShortProbeTimings(t)
	t.Setenv(envVersionGate, "warn")

	binary := writeFakeOpencode(t, `echo "1.18.16"`)

	runtime, err := New(binary, agent.RuntimeAdapterOptions{}, t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v, want warn mode to allow incompatible version", err)
	}
	waitBackgroundProbeEnd(t, runtime)
}

// TestVersionGateResolveIsFirstWriterWins 保证后台探测不会覆盖启动期判定。
func TestVersionGateResolveIsFirstWriterWins(t *testing.T) {
	gate := &versionGate{done: make(chan struct{})}
	gate.resolve(Version{Major: 2, Minor: 1, Patch: 4}, nil)
	gate.resolve(Version{Major: 1, Minor: 18, Patch: 16}, errors.New("late failure"))

	version, err, resolved := gate.wait(time.Second)
	if !resolved {
		t.Fatal("gate should be resolved")
	}
	if err != nil {
		t.Fatalf("gate error = %v, want first result", err)
	}
	if version.String() != "2.1.4" {
		t.Fatalf("gate version = %s, want 2.1.4", version)
	}
}
