// Package opencode adapts the OpenCode CLI to the agent Runtime contract.
// 使用 opencode v2 的 ACP（stdio JSON-RPC）进行通信。
package opencode

// ============================================================================
// OPENCODE_CONFIG_CONTENT 类型
// ============================================================================

// configContent 是 OPENCODE_CONFIG_CONTENT 的顶层结构。
type configContent struct {
	Schema       string                     `json:"$schema,omitempty"`
	Provider     map[string]providerConfig  `json:"provider"`
	Model        string                     `json:"model,omitempty"`
	Agent        map[string]agentConfig     `json:"agent,omitempty"`
	Permission   map[string]any             `json:"permission,omitempty"`
	MCP          map[string]any             `json:"mcp,omitempty"`
	Skills       *skillsConfig              `json:"skills,omitempty"`
	Experimental *experimentalConfig        `json:"experimental,omitempty"`
	DefaultAgent string                     `json:"default_agent,omitempty"`
	Agents       map[string]planAgentConfig `json:"agents,omitempty"`
}

// planAgentConfig 是 v2 原生 primary agent 定义（与 v1 的 agentConfig 不同）。
type planAgentConfig struct {
	Mode        string           `json:"mode"`
	Description string           `json:"description,omitempty"`
	System      string           `json:"system,omitempty"`
	Permissions []permissionRule `json:"permissions,omitempty"`
}

// permissionRule 对应 v2 agents.permissions 的 {action,resource,effect}。
type permissionRule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// experimentalConfig 打开 OpenCode v2 的实验特性；待办在 v2 需要显式开启。
type experimentalConfig struct {
	Todo bool `json:"todo,omitempty"`
}

// skillsConfig configures additional OpenCode Skill search paths.
type skillsConfig struct {
	Paths []string `json:"paths,omitempty"`
}

// agentConfig 描述 OpenCode V1 agent 配置。
type agentConfig struct {
	Prompt string `json:"prompt,omitempty"`
}

// providerConfig 描述一个 AI provider 的配置。
type providerConfig struct {
	ID      string                 `json:"id"`
	Npm     string                 `json:"npm"`
	Options providerOptions        `json:"options"`
	Models  map[string]modelConfig `json:"models"`
}

// providerOptions 包含 provider 的连接配置。
type providerOptions struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseURL"`
	Timeout int    `json:"timeout,omitempty"`
}

// modelConfig 描述单个模型的配置。
type modelConfig struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Limit       modelLimit      `json:"limit"`
	Cost        modelCost       `json:"cost"`
	ToolCall    bool            `json:"tool_call"`
	Attachment  bool            `json:"attachment"`
	Reasoning   bool            `json:"reasoning"`
	Temperature bool            `json:"temperature"`
	Modalities  *modalityConfig `json:"modalities,omitempty"`
	// Options 采样参数等透传项，原样进入请求体 providerOptions（驼峰键）。
	Options map[string]any `json:"options,omitempty"`
}

// modalityConfig 描述模型的模态能力，对齐 opencode config 的 modalities 字段。
// 声明 input 中的某个模态时 opencode 判定 capabilities.input.<modality>=true，
// 对应类型的附件才会传给模型；未声明的模态被降级为提示文本（不整轮失败）。
// 注意：应只声明该模型真正支持的模态——若声明了模型并不支持的类型
// （如视频），opencode 会将其原样传给 AI SDK，后者对不支持的 file part
// 返回硬错误而非优雅降级。output 主要影响模型输出能力的声明
// （目前源码无运行时门控），同样只声明模型实际支持的输出。
type modalityConfig struct {
	Input  []string `json:"input"`
	Output []string `json:"output,omitempty"`
}

// modelLimit 描述模型的上下文限制。
type modelLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// modelCost 描述模型的费用。
type modelCost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}
