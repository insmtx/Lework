package agent

import "testing"

func TestMCPInjectPolicyAppliesTimeoutAndDisabled(t *testing.T) {
	policy := MCPInjectPolicy{TimeoutMS: 1500, Disabled: []string{"Legacy-A", "  dead  ", ""}}

	result := policy.Apply([]MCPServerConfig{
		{Name: "docs"},
		{Name: "dead"},
		{Name: "legacy-a"},
		{Name: "custom", TimeoutMS: 8000},
	})

	if len(result) != 4 {
		t.Fatalf("result length = %d, want 4", len(result))
	}
	for _, config := range result[:3] {
		if config.TimeoutMS != 1500 {
			t.Fatalf("config %q timeout = %d, want policy default 1500", config.Name, config.TimeoutMS)
		}
	}
	if result[3].TimeoutMS != 8000 {
		t.Fatalf("explicit per-server timeout must win, got %d", result[3].TimeoutMS)
	}
	if !result[1].Disabled || !result[2].Disabled {
		t.Fatalf("disabled names should match case-insensitively: %#v", result)
	}
	if result[0].Disabled || result[3].Disabled {
		t.Fatalf("unlisted MCP should stay enabled: %#v", result)
	}
}

func TestMCPInjectPolicyDoesNotMutateInput(t *testing.T) {
	inputs := []MCPServerConfig{{Name: "docs"}}
	MCPInjectPolicy{TimeoutMS: 1500, Disabled: []string{"docs"}}.Apply(inputs)

	if inputs[0].TimeoutMS != 0 || inputs[0].Disabled {
		t.Fatalf("Apply mutated its input: %#v", inputs[0])
	}
}

func TestMCPInjectPolicyEmptyInput(t *testing.T) {
	if got := (MCPInjectPolicy{TimeoutMS: 1500}).Apply(nil); got != nil {
		t.Fatalf("nil input should stay nil, got %#v", got)
	}
}

// 零值策略不得改变任何 MCP 配置，保证未配置时与历史行为一致。
func TestMCPInjectPolicyZeroValueKeepsConfigUnchanged(t *testing.T) {
	inputs := []MCPServerConfig{{Name: "docs", TimeoutMS: 3000}, {Name: "legacy"}}

	result := MCPInjectPolicy{}.Apply(inputs)

	if len(result) != len(inputs) {
		t.Fatalf("result length = %d, want %d", len(result), len(inputs))
	}
	for i, config := range result {
		if config.Name != inputs[i].Name || config.TimeoutMS != inputs[i].TimeoutMS || config.Disabled != inputs[i].Disabled {
			t.Fatalf("config %d changed: got %#v want %#v", i, config, inputs[i])
		}
	}
}

// 已标记禁用的 MCP 不应被策略重新启用，且显式超时保留。
func TestMCPInjectPolicyPreservesExplicitDisabled(t *testing.T) {
	inputs := []MCPServerConfig{{Name: "docs", Disabled: true, TimeoutMS: 2500}}

	result := MCPInjectPolicy{TimeoutMS: 1500, Disabled: []string{"other"}}.Apply(inputs)

	if !result[0].Disabled || result[0].TimeoutMS != 2500 {
		t.Fatalf("explicit fields should be preserved: %#v", result[0])
	}
}
