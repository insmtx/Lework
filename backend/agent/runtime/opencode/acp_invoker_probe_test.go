package opencode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/internal/cli"
)

// TestACPInvokerProbe 是 W5 的端到端验收：真实 opencode ACP 走完
// spawn → session → prompt → 事件流 → 结果，验证 invoker 装配正确。
//
//	LEROS_ACP_PROBE=1 LEROS_ACP_PROBE_CONFIG_FILE=... \
//	  LEROS_ACP_PROBE_MODEL=magpie/yygu/deepseek-flash \
//	  go test ./backend/agent/runtime/opencode -run TestACPInvokerProbe -v
func TestACPInvokerProbe(t *testing.T) {
	if os.Getenv("LEROS_ACP_PROBE") != "1" {
		t.Skip("manual probe: set LEROS_ACP_PROBE=1 (needs opencode 2.1.x and network)")
	}
	modelCfg, err := probeModelConfig()
	if err != nil {
		t.Fatalf("load probe model: %v", err)
	}

	root := t.TempDir()
	workDir := filepath.Join(root, "workspace")
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	invoker := NewACPInvoker("opencode", nil, dataDir, 2*time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	handle, err := invoker.Invoke(ctx, cli.InvocationRequest{
		ExecutionID: "probe-acp-invoker",
		WorkDir:     workDir,
		Prompt:      "只回答两个字：收到",
		Model:       modelCfg,
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if handle.Process == nil || handle.Process.PID() == 0 {
		t.Fatal("process handle missing pid")
	}
	defer handle.Process.Stop()

	types := map[agent.NodeEventType]int{}
	for event := range handle.Events {
		types[event.Type]++
	}
	result := <-handle.Result
	if result.Err != nil {
		t.Fatalf("result error: %v (events=%v)", result.Err, types)
	}
	if strings.TrimSpace(result.Message) == "" {
		t.Fatalf("empty final message (events=%v)", types)
	}
	if result.ProviderSessionID == "" {
		t.Fatal("provider session id missing")
	}
	if result.Usage == nil || result.Usage.InputTokens <= 0 {
		t.Fatalf("usage = %+v, want input tokens", result.Usage)
	}
	if types[agent.NodeEventAgentStart] != 1 {
		t.Fatalf("agent.start count = %d, want 1", types[agent.NodeEventAgentStart])
	}
	if types[agent.NodeEventMessageUpdate] == 0 {
		t.Fatalf("message.update missing: %v", types)
	}
	if types[agent.NodeEventMessageEnd] != 1 {
		t.Fatalf("message.end count = %d, want 1", types[agent.NodeEventMessageEnd])
	}
	t.Logf("result message=%q session=%s usage=%+v events=%v",
		result.Message, result.ProviderSessionID, result.Usage, types)
}

type probeProviderFile struct {
	Provider map[string]struct {
		Options struct {
			APIKey  string `json:"apiKey"`
			BaseURL string `json:"baseURL"`
		} `json:"options"`
	} `json:"provider"`
}

func probeModelConfig() (agent.ModelConfig, error) {
	path := strings.TrimSpace(os.Getenv("LEROS_ACP_PROBE_CONFIG_FILE"))
	model := strings.TrimSpace(os.Getenv("LEROS_ACP_PROBE_MODEL"))
	if path == "" || model == "" {
		return agent.ModelConfig{}, os.ErrNotExist
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return agent.ModelConfig{}, err
	}
	var file probeProviderFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return agent.ModelConfig{}, err
	}
	providerID, modelID, ok := strings.Cut(model, "/")
	if !ok {
		return agent.ModelConfig{}, os.ErrInvalid
	}
	provider, ok := file.Provider[providerID]
	if !ok {
		return agent.ModelConfig{}, os.ErrNotExist
	}
	return agent.ModelConfig{
		Model:   modelID,
		APIKey:  provider.Options.APIKey,
		BaseURL: provider.Options.BaseURL,
	}, nil
}
