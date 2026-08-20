package types

// DisabledPluginKind identifies the capability family disabled for one run.
type DisabledPluginKind string

const (
	// DisabledPluginKindSkill disables a Skill by its catalog code.
	DisabledPluginKindSkill DisabledPluginKind = "skill"
	// DisabledPluginKindMCP disables an MCP plugin by its plugin code.
	DisabledPluginKindMCP DisabledPluginKind = "mcp"
)

// DisabledPlugin identifies one Skill or MCP capability excluded from a Run.
// Code is used instead of a database ID so system Skills without a persisted
// Plugin identity can use the same execution policy as project plugins.
type DisabledPlugin struct {
	Kind DisabledPluginKind `json:"kind"`
	Code string             `json:"code"`
}
