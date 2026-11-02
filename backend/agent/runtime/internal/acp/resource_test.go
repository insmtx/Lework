package acp

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestOpenCodeACPResources 记录 W1-3 的资源量测：
// 冷启动到 Prompt 可用耗时、首 token 耗时、进程数、两进程 RSS 峰值之和。
// 不做硬性阈值断言，只要求量到非零值（数值随机器与模型变化）。
//
//	LEWORK_ACP_PROBE=1 LEWORK_ACP_PROBE_CONFIG_FILE=... \
//	  go test ./backend/agent/runtime/internal/acp -run TestOpenCodeACPResources -v
func TestOpenCodeACPResources(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1 (needs opencode 2.1.x and network)")
	}

	binary := envOrDefault("LEWORK_ACP_PROBE_BIN", "opencode")
	model := envOrDefault("LEWORK_ACP_PROBE_MODEL", "opencode/glm-5.3-flash-free")

	root, workDir := newProbeWorkspace(t)
	configContent, err := probeConfigContent(model)
	if err != nil {
		t.Fatalf("build probe config: %v", err)
	}
	env := append(probeBaseEnv(root), "OPENCODE_CONFIG_CONTENT="+configContent)

	startAt := time.Now()
	conn, err := Start(context.Background(), Config{
		Binary: binary,
		Dir:    workDir,
		Env:    env,
	})
	if err != nil {
		t.Fatalf("start acp: %v", err)
	}
	defer conn.Close()
	startupDur := time.Since(startAt)

	sampler := newProcessSampler(conn.PID())
	defer sampler.Stop()

	sessionAt := time.Now()
	sess, err := conn.NewSession(context.Background(), workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	sessionDur := time.Since(sessionAt)
	readyDur := startupDur + sessionDur

	baseline := len(conn.Client().Snapshot().Text)
	firstToken := time.Duration(-1)
	stopPolling := make(chan struct{})
	var pollWG sync.WaitGroup
	promptStart := time.Now()
	pollWG.Add(1)
	go func() {
		defer pollWG.Done()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopPolling:
				return
			case <-ticker.C:
				if len(conn.Client().Snapshot().Text) > baseline {
					firstToken = time.Since(promptStart)
					return
				}
			}
		}
	}()

	resp, err := sess.Prompt(context.Background(), "请用三句话介绍 Go 语言的特点。")
	promptDur := time.Since(promptStart)
	close(stopPolling)
	pollWG.Wait()
	if err != nil {
		t.Fatalf("prompt: %v%s", err, stderrSuffix(conn))
	}

	peakRSSKB, peakProcs := sampler.Stop()
	peakRSSMB := float64(peakRSSKB) / 1024

	t.Logf("startup(spawn+initialize)=%s session/new=%s ready_to_prompt=%s",
		startupDur.Round(time.Millisecond), sessionDur.Round(time.Millisecond), readyDur.Round(time.Millisecond))
	inputTokens := 0
	if resp.Usage != nil {
		inputTokens = resp.Usage.InputTokens
	}
	t.Logf("first_token=%s prompt_total=%s stop=%s input_tokens=%d",
		firstToken.Round(time.Millisecond), promptDur.Round(time.Millisecond), resp.StopReason, inputTokens)
	snapshot := conn.Client().Snapshot()
	t.Logf("final_text=%q baseline=%d kinds=%v", truncate(snapshot.Text, 40), baseline, snapshot.Kinds)
	t.Logf("peak_processes=%d peak_rss_sum=%.1f MB", peakProcs, peakRSSMB)

	if peakRSSKB <= 0 {
		t.Fatalf("failed to sample RSS from process tree (root pid %d)", conn.PID())
	}
	if peakProcs < 2 {
		t.Fatalf("expected supervisor + private serve, observed peak %d process(es)", peakProcs)
	}
	if firstToken < 0 {
		// 极短回复可能只在 Prompt 返回前一次性落齐，轮询拿不到增量；
		// 这属于度量盲区，不影响 RSS/进程数结论。
		t.Logf("first_token unavailable (text observed only at completion)")
	}
}

// processSampler 每 50ms 采样一次进程树（supervisor + 私有 serve）的 RSS 之和。
type processSampler struct {
	root int

	mu        sync.Mutex
	peakRSSKB int64
	peakProcs int
	stopCh    chan struct{}
	doneCh    chan struct{}
	stopOnce  sync.Once
}

func newProcessSampler(root int) *processSampler {
	s := &processSampler{root: root, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
	go s.loop()
	return s
}

func (s *processSampler) loop() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	defer close(s.doneCh)
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.sample()
		}
	}
}

func (s *processSampler) sample() {
	pids := descendantPIDs(s.root)
	if len(pids) == 0 {
		return
	}
	args := []string{"-o", "rss=", "-p"}
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	args = append(args, strings.Join(parts, ","))
	out, err := exec.Command("ps", args...).Output()
	if err != nil {
		return
	}
	var sum int64
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		rss, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		sum += rss
		count++
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sum > s.peakRSSKB {
		s.peakRSSKB = sum
	}
	if count > s.peakProcs {
		s.peakProcs = count
	}
}

// Stop 停止采样并返回峰值（KB 与进程数），可安全重复调用。
func (s *processSampler) Stop() (peakRSSKB int64, peakProcs int) {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		<-s.doneCh
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakRSSKB, s.peakProcs
}

func descendantPIDs(root int) []int {
	if root <= 0 {
		return nil
	}
	result := []int{root}
	queue := []int{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		out, err := exec.Command("pgrep", "-P", strconv.Itoa(parent)).Output()
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(out)) {
			pid, err := strconv.Atoi(field)
			if err != nil {
				continue
			}
			result = append(result, pid)
			queue = append(queue, pid)
		}
	}
	return result
}
