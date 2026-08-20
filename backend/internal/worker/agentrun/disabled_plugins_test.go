package agentrun

import (
	"context"
	"testing"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/types"
)

func TestApplyDisabledPluginPolicyFiltersSkillAndMCPSnapshots(t *testing.T) {
	req := &agentrundomain.RunRequest{
		Policy: agentrundomain.PolicyContext{DisabledPlugins: []types.DisabledPlugin{
			{Kind: types.DisabledPluginKindSkill, Code: " Review "},
			{Kind: types.DisabledPluginKindMCP, Code: "calendar"},
		}},
		Plugins: []agentrundomain.PluginSnapshot{
			{Code: "review", Kind: "skill"},
			{Code: "calendar", Kind: "mcp"},
			{Code: "keep", Kind: "skill"},
		},
	}

	policy := applyDisabledPluginPolicy(context.Background(), req)
	if len(req.Plugins) != 1 || req.Plugins[0].Code != "keep" {
		t.Fatalf("filtered plugins = %#v, want only keep", req.Plugins)
	}
	if !policy.skillDisabled("review") || !policy.mcpDisabled("CALENDAR") {
		t.Fatalf("normalized policy = %#v", policy)
	}
	if len(req.Policy.DisabledPlugins) != 2 {
		t.Fatalf("normalized disabled plugins = %#v", req.Policy.DisabledPlugins)
	}
}
