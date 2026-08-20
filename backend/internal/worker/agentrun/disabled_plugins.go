package agentrun

import (
	"context"
	"sort"
	"strings"

	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// disabledPluginPolicy is the normalized, run-local view of DisabledPlugins.
// Skill codes and MCP plugin codes share one transport shape but are kept in
// separate sets so disabling an embedded Skill does not remove its MCP.
type disabledPluginPolicy struct {
	skillCodes map[string]struct{}
	mcpCodes   map[string]struct{}
}

func normalizeDisabledPluginPolicy(ctx context.Context, req *agentrundomain.RunRequest) disabledPluginPolicy {
	policy := disabledPluginPolicy{
		skillCodes: make(map[string]struct{}),
		mcpCodes:   make(map[string]struct{}),
	}
	if req == nil {
		return policy
	}
	for _, disabled := range req.Policy.DisabledPlugins {
		code := strings.ToLower(strings.TrimSpace(disabled.Code))
		kind := types.DisabledPluginKind(strings.ToLower(strings.TrimSpace(string(disabled.Kind))))
		if code == "" {
			logs.WarnContextf(ctx, "skip disabled plugin policy with empty code")
			continue
		}
		switch kind {
		case types.DisabledPluginKindSkill:
			policy.skillCodes[code] = struct{}{}
		case types.DisabledPluginKindMCP:
			policy.mcpCodes[code] = struct{}{}
		default:
			logs.WarnContextf(ctx, "skip disabled plugin policy with unsupported kind=%q code=%q", kind, code)
		}
	}
	return policy
}

func (p disabledPluginPolicy) skillDisabled(code string) bool {
	_, ok := p.skillCodes[strings.ToLower(strings.TrimSpace(code))]
	return ok
}

func (p disabledPluginPolicy) mcpDisabled(code string) bool {
	_, ok := p.mcpCodes[strings.ToLower(strings.TrimSpace(code))]
	return ok
}

func (p disabledPluginPolicy) addSkill(code string) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code != "" {
		p.skillCodes[code] = struct{}{}
	}
}

func (p disabledPluginPolicy) entries() []types.DisabledPlugin {
	entries := make([]types.DisabledPlugin, 0, len(p.skillCodes)+len(p.mcpCodes))
	for code := range p.skillCodes {
		entries = append(entries, types.DisabledPlugin{Kind: types.DisabledPluginKindSkill, Code: code})
	}
	for code := range p.mcpCodes {
		entries = append(entries, types.DisabledPlugin{Kind: types.DisabledPluginKindMCP, Code: code})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Code < entries[j].Code
	})
	return entries
}

// applyDisabledPluginPolicy removes disabled project capabilities from the
// immutable Run snapshot and expands disabled MCP entries to any embedded
// Skill code before the runtime and Skill preparers consume the request.
func applyDisabledPluginPolicy(ctx context.Context, req *agentrundomain.RunRequest) disabledPluginPolicy {
	policy := normalizeDisabledPluginPolicy(ctx, req)
	if req == nil {
		return policy
	}
	filtered := make([]agentrundomain.PluginSnapshot, 0, len(req.Plugins))
	for _, snapshot := range req.Plugins {
		kind := strings.ToLower(strings.TrimSpace(snapshot.Kind))
		switch {
		case kind == string(types.DisabledPluginKindMCP) && policy.mcpDisabled(snapshot.Code):
			if descriptor, err := pluginSnapshotSkill(snapshot); err == nil && descriptor != nil {
				policy.addSkill(descriptor.Code)
			}
			logs.InfoContextf(ctx, "disabled MCP plugin for run: code=%s", snapshot.Code)
		case kind == string(types.DisabledPluginKindSkill) && policy.skillDisabled(snapshot.Code):
			logs.InfoContextf(ctx, "disabled Skill plugin for run: code=%s", snapshot.Code)
		default:
			filtered = append(filtered, snapshot)
		}
	}
	req.Plugins = filtered
	req.Policy.DisabledPlugins = policy.entries()
	return policy
}
