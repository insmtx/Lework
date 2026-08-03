package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

const (
	// MCPChannelStatusActive allows a channel to be displayed and used for new connections.
	MCPChannelStatusActive = "active"
	// MCPChannelStatusInactive keeps a channel configuration without allowing new connections.
	MCPChannelStatusInactive = "inactive"
	// MCPChannelAuthTypeNone requires no user authorization.
	MCPChannelAuthTypeNone = "none"
	// MCPChannelAuthTypeForm collects connector values from a schema-driven form.
	MCPChannelAuthTypeForm = "form"
	// MCPChannelAuthTypeOAuth reserves an OAuth authorization flow for channels that support it.
	MCPChannelAuthTypeOAuth = "oauth"
	// MCPChannelAuthTypeManaged delegates credential creation to a server-side handler.
	MCPChannelAuthTypeManaged = "managed"
)

// MCPChannelHeaders stores non-sensitive fixed HTTP headers for one MCP channel.
type MCPChannelHeaders map[string]string

// Scan implements sql.Scanner.
func (h *MCPChannelHeaders) Scan(value interface{}) error {
	if value == nil {
		*h = MCPChannelHeaders{}
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("cannot scan %T into MCPChannelHeaders", value)
	}
	result := make(map[string]string)
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	*h = result
	return nil
}

// Value implements driver.Valuer and always stores a JSON object.
func (h MCPChannelHeaders) Value() (driver.Value, error) {
	if len(h) == 0 {
		return "{}", nil
	}
	return json.Marshal(map[string]string(h))
}

// MCPChannelAuthField describes one user-provided authorization value.
type MCPChannelAuthField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
	Description string `json:"description,omitempty"`
}

// MCPChannelAuthBindings maps stored value keys into runtime destinations.
type MCPChannelAuthBindings struct {
	SkillEnv       map[string]string `json:"skill_env,omitempty"`
	MCPBearerToken string            `json:"mcp_bearer_token,omitempty"`
	MCPHeaders     map[string]string `json:"mcp_headers,omitempty"`
	MCPEnv         map[string]string `json:"mcp_env,omitempty"`
	MCPQuery       map[string]string `json:"mcp_query,omitempty"`
}

// MCPChannelOAuthConfig stores operations-managed OAuth application settings.
type MCPChannelOAuthConfig struct {
	AppKey      string   `json:"app_key,omitempty"`
	SecretKey   string   `json:"secret_key,omitempty"`
	RedirectURI string   `json:"redirect_uri,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

// MCPChannelAuthConfig defines channel-specific authorization without storing user credentials.
type MCPChannelAuthConfig struct {
	Fields   []MCPChannelAuthField  `json:"fields,omitempty"`
	Bindings MCPChannelAuthBindings `json:"bindings,omitempty"`
	Handler  string                 `json:"handler,omitempty"`
	OAuth    *MCPChannelOAuthConfig `json:"oauth,omitempty"`
}

// MCPChannelAuthConfigJSON stores one typed channel authorization schema.
type MCPChannelAuthConfigJSON MCPChannelAuthConfig

// Scan implements sql.Scanner.
func (c *MCPChannelAuthConfigJSON) Scan(value interface{}) error {
	if value == nil {
		*c = MCPChannelAuthConfigJSON{}
		return nil
	}
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	default:
		return fmt.Errorf("cannot scan %T into MCPChannelAuthConfigJSON", value)
	}
	var result MCPChannelAuthConfig
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	*c = MCPChannelAuthConfigJSON(result)
	return nil
}

// Value implements driver.Valuer and always stores a JSON object.
func (c MCPChannelAuthConfigJSON) Value() (driver.Value, error) {
	return json.Marshal(MCPChannelAuthConfig(c))
}

// MCPChannel is a system-maintained template for one built-in connector channel.
type MCPChannel struct {
	gorm.Model

	Channel     string                   `gorm:"column:channel;type:varchar(64);not null;uniqueIndex:ux_mcp_channel_channel"`
	Name        string                   `gorm:"column:name;type:varchar(255);not null"`
	Description string                   `gorm:"column:description;type:text"`
	SkillCode   string                   `gorm:"column:skill_code;type:varchar(128);not null;default:''"`
	Transport   string                   `gorm:"column:transport;type:varchar(32);not null;default:''"`
	URL         string                   `gorm:"column:url;type:varchar(2000);not null;default:''"`
	Headers     MCPChannelHeaders        `gorm:"column:headers;type:jsonb;not null;default:'{}'"`
	AuthType    string                   `gorm:"column:auth_type;type:varchar(32);not null;default:'none'"`
	AuthConfig  MCPChannelAuthConfigJSON `gorm:"column:auth_config;type:jsonb;not null;default:'{}'"`
	Status      string                   `gorm:"column:status;type:varchar(32);not null;default:'active';index"`
}

// TableName returns the MCP channel configuration table name.
func (MCPChannel) TableName() string {
	return TableNameMCPChannel
}
