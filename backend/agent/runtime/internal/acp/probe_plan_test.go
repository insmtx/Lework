package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestOpenCodeACPPlanScope 验证 W1-2 的计划模式注入与权限隔离：
//  1. 自定义 primary agent `lework-plan` 进入 configOptions.mode 并可被 default_agent 选中；
//  2. `.opencode/plans/*` 可写；
//  3. 工作区外写入被拒（配置 deny + 客户端 reject 双保险）。
//
// 默认跳过，运行方式见 TestOpenCodeACPProbe 的注释；需要带 provider 凭据的
// LEWORK_ACP_PROBE_CONFIG_FILE。
func TestOpenCodeACPPlanScope(t *testing.T) {
	if os.Getenv("LEWORK_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEWORK_ACP_PROBE=1 (needs opencode 2.1.x and network)")
	}

	binary := envOrDefault("LEWORK_ACP_PROBE_BIN", "opencode")
	model := envOrDefault("LEWORK_ACP_PROBE_MODEL", "opencode/glm-5.3-flash-free")

	root, workDir := newProbeWorkspace(t)
	plansDir := envOrDefault("LEWORK_ACP_PLAN_TARGET_DIR", filepath.Join(workDir, ".opencode", "plans"))
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatalf("create plans dir: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(plansDir); err == nil {
		plansDir = resolved
	}
	planFile := filepath.Join(plansDir, "probe-plan.md")
	outsideFile := filepath.Join(t.TempDir(), "outside.md")

	cfg, err := probeConfigMap(model)
	if err != nil {
		t.Fatalf("build probe config: %v", err)
	}
	// 实测（W1-2）：工作区内目标的权限 resource 用「相对 cwd 的 glob」才命中；
	// 绝对路径只在目标位于工作区外时生效。详见设计文档 §8.2 的实测修正。
	allowPattern := envOrDefault("LEWORK_ACP_PLAN_ALLOW_PATTERN", ".opencode/plans/*")
	allowPattern = strings.NewReplacer("$PLANSDIR", plansDir, "$WORKDIR", workDir).Replace(allowPattern)

	cfg["default_agent"] = "lework-plan"
	cfg["agents"] = map[string]any{
		"lework-plan": map[string]any{
			"mode":        "primary",
			"description": "Lework plan mode",
			"system":      "You are the Lework plan agent. Write plan files when asked.",
			"permissions": []any{
				map[string]any{"action": "question", "resource": "*", "effect": "allow"},
				map[string]any{"action": "read", "resource": "*", "effect": "allow"},
				map[string]any{"action": "edit", "resource": "*", "effect": "deny"},
				map[string]any{"action": "shell", "resource": "*", "effect": "deny"},
				map[string]any{
					"action":   "edit",
					"resource": allowPattern,
					"effect":   "allow",
				},
			},
		},
	}
	content, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal probe config: %v", err)
	}
	env := append(probeBaseEnv(root), "OPENCODE_CONFIG_CONTENT="+string(content))

	// 诊断项：上游 debug agents 在自定义 agent/冷启动下不稳定，
	// 这里只记录，不作为硬断言；权威信号是 configOptions.mode。
	t.Run("diagnostic_debug_agents", func(t *testing.T) {
		ids := listDebugAgentIDs(t, env, workDir)
		if !containsString(ids, "lework-plan") {
			t.Skipf("debug agents did not list lework-plan (got %v); "+
				"上游在 isolated XDG 下不列自定义 agent，改用 configOptions.mode 验证", ids)
		}
		t.Logf("debug agents includes lework-plan: %v", ids)
	})

	frameSink, closeSink := openFrameSinkNamed(t, "opencode-2.1.4-plan.ndjson")
	if closeSink != nil {
		defer closeSink()
	}

	conn, err := Start(context.Background(), Config{
		Binary:           binary,
		Dir:              workDir,
		Env:              env,
		FrameSink:        frameSink,
		PermissionPolicy: "reject",
	})
	if err != nil {
		t.Fatalf("start acp: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	sess, err := conn.NewSession(ctx, workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := sess.RequireModel(model); err != nil {
		t.Fatalf("%v", err)
	}

	t.Run("check1_mode_selected", func(t *testing.T) {
		mode, ok := sess.ModeCurrentValue()
		if !ok {
			t.Fatalf("configOptions has no mode option")
		}
		if mode != "lework-plan" {
			t.Fatalf("mode currentValue=%q, want lework-plan", mode)
		}
		values := sess.ConfigOptionValues("mode")
		if !containsString(values, "lework-plan") {
			t.Fatalf("mode values=%v do not contain lework-plan", values)
		}
		t.Logf("mode=%s values=%v", mode, values)
	})

	t.Run("check2_plan_file_writable", func(t *testing.T) {
		_, err := sess.Prompt(ctx,
			"请用 write 工具把下面这段计划原样写入文件 "+planFile+"：\n"+
				"# probe plan\n- step 1\n"+
				"不要提问，直接写文件。")
		if err != nil {
			t.Fatalf("plan write prompt: %v%s", err, stderrSuffix(conn))
		}
		info, err := os.Stat(planFile)
		if err != nil {
			t.Fatalf("plan file was not written: %v; %s", err, describeToolCalls(conn))
		}
		if info.Size() == 0 {
			t.Fatalf("plan file is empty; %s", describeToolCalls(conn))
		}
		t.Logf("plan file written: %s (%d bytes)", planFile, info.Size())
	})

	t.Run("check3_outside_write_denied", func(t *testing.T) {
		before := conn.Client().Snapshot()
		_, err := sess.Prompt(ctx,
			"请只用 write 工具把同样的内容写入 "+outsideFile+"。"+
				"不要用 bash/shell，不要提问，直接写。")
		if err != nil {
			t.Fatalf("outside write prompt: %v%s", err, stderrSuffix(conn))
		}
		snapshot := conn.Client().Snapshot()
		t.Logf("permission requests: before=%d after=%d; %s",
			len(before.Permissions), len(snapshot.Permissions), describeToolCalls(conn))
		if _, err := os.Stat(outsideFile); err == nil {
			t.Fatalf("outside file was written despite deny rule: %s", outsideFile)
		}
	})
}

func describeToolCalls(conn *Conn) string {
	snapshot := conn.Client().Snapshot()
	parts := make([]string, 0, len(snapshot.ToolCalls))
	for _, call := range snapshot.ToolCalls {
		input, _ := json.Marshal(call.RawInput)
		output, _ := json.Marshal(call.RawOutput)
		parts = append(parts, fmt.Sprintf("%s/%s/%s input=%s output=%s",
			call.Kind, call.Status, call.Title, truncate(string(input), 160), truncate(string(output), 160)))
	}
	return "toolCalls=[" + strings.Join(parts, ", ") + "]"
}

// listDebugAgentIDs 在隔离 XDG 下启动 managed service 并读取 agent 列表。
// 首次调用可能因服务冷启动返回空列表，这里重试 3 次。
func listDebugAgentIDs(t *testing.T, env []string, dir string) []string {
	t.Helper()
	baseEnv := append(os.Environ(), env...)
	port := 49200 + int(time.Now().UnixNano()%700)
	run := func(args ...string) (string, error) {
		cmd := exec.Command("opencode", args...)
		cmd.Dir = dir
		cmd.Env = baseEnv
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if _, err := run("service", "set", "port", strconv.Itoa(port)); err != nil {
		t.Logf("service set port: %v", err)
	}
	defer func() { _, _ = run("service", "stop") }()

	var ids []string
	for attempt := 0; attempt < 3; attempt++ {
		out, err := run("debug", "agents")
		if err != nil {
			t.Logf("debug agents attempt %d: %v", attempt+1, err)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		var agents []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(out), &agents); err != nil {
			t.Logf("debug agents output is not JSON: %.120s", out)
			continue
		}
		ids = ids[:0]
		for _, agent := range agents {
			ids = append(ids, agent.ID)
		}
		if len(ids) > 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	return ids
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
