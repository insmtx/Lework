package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Lework/backend/agent"
)

// TestOpenCodeACPPermissionProbe 验证权限桥的真机往返：
// 配置 edit=ask 后，write 工具触发 session/request_permission，
// Bridge 产出 approval.requested；approve 落盘、deny 不落盘。
func TestOpenCodeACPPermissionProbe(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1")
	}
	model := envOrDefault("LEWORK_ACP_PROBE_MODEL", "opencode/glm-5.3-flash-free")

	root, workDir := newProbeWorkspace(t)
	cfg, err := probeConfigMap(model)
	if err != nil {
		t.Fatalf("build probe config: %v", err)
	}
	cfg["permission"] = map[string]any{"edit": "ask"}
	content, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	env := append(probeBaseEnv(root), "OPENCODE_CONFIG_CONTENT="+string(content))

	frameSink, closeSink := openFrameSinkNamed(t, "opencode-2.1.4-permission.ndjson")
	if closeSink != nil {
		defer closeSink()
	}
	conn, err := Start(context.Background(), Config{
		Binary:    envOrDefault("LEWORK_ACP_PROBE_BIN", "opencode"),
		Dir:       workDir,
		Env:       env,
		FrameSink: frameSink,
	})
	if err != nil {
		t.Fatalf("start acp: %v", err)
	}
	defer conn.Close()

	approvals := make(chan agent.NodeEvent, 4)
	bridge := NewPermissionBridge("probe", func(event agent.NodeEvent) error {
		approvals <- event
		return nil
	})
	conn.Client().SetPermissionBridge(bridge)

	ctx := context.Background()
	sess, err := conn.NewSession(ctx, workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := sess.RequireModel(model); err != nil {
		t.Fatalf("%v", err)
	}

	approvedPath := filepath.Join(workDir, "approved.txt")
	deniedPath := filepath.Join(workDir, "denied.txt")

	runPrompt := func(prompt string, action string) {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			_, err := sess.Prompt(ctx, prompt)
			done <- err
		}()

		var event agent.NodeEvent
		select {
		case event = <-approvals:
		case <-time.After(20 * time.Second):
			t.Fatalf("no approval.requested for %s", prompt)
		}
		payload, ok := event.Payload.(*agent.ApprovalRequestedPayload)
		if !ok || payload.RequestID == "" {
			t.Fatalf("approval payload = %+v", event.Payload)
		}
		if payload.ToolCallID == "" {
			t.Fatalf("approval toolCallId missing: %+v", payload)
		}
		if payload.ToolName != "write" {
			t.Fatalf("approval tool name = %q, want write (kind over title)", payload.ToolName)
		}
		if err := bridge.WriteDecision(payload.RequestID, action); err != nil {
			t.Fatalf("write decision: %v", err)
		}
		select {
		case err := <-done:
			if err != nil && !strings.Contains(err.Error(), "denied") {
				t.Logf("prompt result: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("prompt did not return after decision")
		}
		t.Logf("approval tool=%s decision=%s", payload.ToolName, action)
	}

	runPrompt("请用 write 工具把 hello 写入文件 "+approvedPath+"，不要用 bash。", agent.ApprovalActionApprove)
	if _, err := os.Stat(approvedPath); err != nil {
		t.Fatalf("approved write missing: %v", err)
	}
	snapshot := conn.Client().Snapshot()
	t.Logf("permission requests after approve: %d", len(snapshot.Permissions))

	runPrompt("请用 write 工具把 hello 写入文件 "+deniedPath+"，不要用 bash。", agent.ApprovalActionDeny)
	if _, err := os.Stat(deniedPath); err == nil {
		t.Fatalf("denied write was created: %s", deniedPath)
	}
}
