package acp

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// TestOpenCodeACPCancelLatency 验证真机取消收敛：ctx 取消后 Prompt 必须在 8s 内返回。
func TestOpenCodeACPCancelLatency(t *testing.T) {
	if os.Getenv("LEROS_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEROS_ACP_PROBE=1")
	}
	model := envOrDefault("LEROS_ACP_PROBE_MODEL", "opencode/glm-5.3-flash-free")
	root, workDir := newProbeWorkspace(t)
	content, err := probeConfigContent(model)
	if err != nil {
		t.Fatalf("build config: %v", err)
	}
	env := append(probeBaseEnv(root), "OPENCODE_CONFIG_CONTENT="+content)

	conn, err := Start(context.Background(), Config{
		Binary: envOrDefault("LEROS_ACP_PROBE_BIN", "opencode"),
		Dir:    workDir,
		Env:    env,
	})
	if err != nil {
		t.Fatalf("start acp: %v", err)
	}
	defer conn.Close()
	sess, err := conn.NewSession(context.Background(), workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	started := time.Now()
	_, promptErr := sess.Prompt(ctx, "请用至少八百字介绍 Go 语言的历史、设计取舍与典型应用场景。")
	elapsed := time.Since(started)

	if !errors.Is(promptErr, context.Canceled) {
		t.Fatalf("prompt error = %v, want context.Canceled", promptErr)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("cancel converged in %s, want <= 8s", elapsed)
	}
	t.Logf("cancel converged in %s", elapsed.Round(time.Millisecond))
}
