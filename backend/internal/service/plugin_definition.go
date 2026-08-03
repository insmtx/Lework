package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// ArtifactDefinition identifies an immutable bundle through its FileUpload record.
type ArtifactDefinition struct {
	FileUploadID string `json:"file_upload_id"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
	ContentType  string `json:"content_type"`
}

// SkillSourceDefinition identifies a non-bundle Skill source such as GitHub.
type SkillSourceDefinition struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type skillDefinition struct {
	Schema   string                 `json:"schema"`
	Artifact *ArtifactDefinition    `json:"artifact"`
	Source   *SkillSourceDefinition `json:"source"`
}

// MCPDefinition is the immutable MCP configuration stored in a plugin revision.
type MCPDefinition struct {
	Schema        string            `json:"schema"`
	Transport     string            `json:"transport"`
	Name          string            `json:"name"`
	Provider      string            `json:"provider,omitempty"`
	URL           string            `json:"url,omitempty"`
	BearerToken   string            `json:"bearer_token,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	SecretRefs    map[string]string `json:"secret_refs,omitempty"`
	EnvSecretRefs map[string]string `json:"env_secret_refs,omitempty"`
}

const (
	ConnectorModeSkillOnly                = "skill_only"
	ConnectorModeMCPOnly                  = "mcp_only"
	ConnectorModeHybrid                   = "hybrid"
	ConnectorOAuthPending                 = "pending"
	ConnectorOAuthExchanging              = "exchanging"
	ConnectorOAuthActive                  = "active"
	ConnectorOAuthFailed                  = "failed"
	ConnectorOAuthReauthorizationRequired = "reauthorization_required"
)

// ConnectorSkillDefinition identifies the Skill bundled with a connector.
type ConnectorSkillDefinition struct {
	Code     string              `json:"code"`
	Revision int                 `json:"revision"`
	Artifact *ArtifactDefinition `json:"artifact"`
}

// ConnectorAuthDefinition stores one immutable authorization snapshot.
// Values remain plaintext until a dedicated credential store is introduced.
type ConnectorAuthDefinition struct {
	Type     string                       `json:"type"`
	Values   map[string]string            `json:"values,omitempty"`
	Bindings types.MCPChannelAuthBindings `json:"bindings,omitempty"`
	OAuth    *ConnectorOAuthDefinition    `json:"oauth,omitempty"`
}

// ConnectorOAuthDefinition records one connector OAuth state without exposing application secrets.
type ConnectorOAuthDefinition struct {
	Status            string    `json:"status"`
	AttemptID         string    `json:"attempt_id,omitempty"`
	StateHash         string    `json:"state_hash,omitempty"`
	StateExpiresAt    time.Time `json:"state_expires_at,omitempty"`
	TokenExpiresAt    time.Time `json:"token_expires_at,omitempty"`
	Scopes            []string  `json:"scopes,omitempty"`
	ExternalAccountID string    `json:"external_account_id,omitempty"`
	DisplayName       string    `json:"display_name,omitempty"`
	ErrorCode         string    `json:"error_code,omitempty"`
}

// ConnectorDefinition composes optional Skill and MCP capabilities in one plugin revision.
type ConnectorDefinition struct {
	Schema  string                    `json:"schema"`
	Channel string                    `json:"channel"`
	Mode    string                    `json:"mode"`
	Auth    ConnectorAuthDefinition   `json:"auth"`
	Skill   *ConnectorSkillDefinition `json:"skill,omitempty"`
	MCP     *MCPDefinition            `json:"mcp,omitempty"`
}

type workflowDefinition struct {
	Schema     string          `json:"schema"`
	Definition json.RawMessage `json:"definition"`
}

// ValidatePluginDefinition validates the JSON stored and transported for one plugin kind.
func ValidatePluginDefinition(kind string, raw json.RawMessage) error {
	if !json.Valid(raw) {
		return fmt.Errorf("definition must be valid JSON")
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "skill":
		var value skillDefinition
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if value.Schema != "skill/v1" {
			return fmt.Errorf("unsupported skill definition schema %q", value.Schema)
		}
		if value.Artifact != nil && strings.TrimSpace(value.Artifact.FileUploadID) != "" && strings.TrimSpace(value.Artifact.SHA256) != "" {
			return nil
		}
		if value.Source != nil && strings.EqualFold(value.Source.Type, "github") && strings.TrimSpace(value.Source.URL) != "" {
			return nil
		}
		return fmt.Errorf("skill definition requires artifact file_upload_id and sha256, or a github source")
	case "mcp":
		var envelope struct {
			Schema string `json:"schema"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return err
		}
		if envelope.Schema == "connector/v1" {
			return validateConnectorDefinition(raw)
		}
		var value MCPDefinition
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if value.Schema != "mcp/v1" {
			return fmt.Errorf("unsupported mcp definition schema %q", value.Schema)
		}
		return validateMCPDefinition(value)
	case "workflow":
		var value workflowDefinition
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if value.Schema != "workflow/v1" {
			return fmt.Errorf("unsupported workflow definition schema %q", value.Schema)
		}
		if !json.Valid(value.Definition) || len(value.Definition) == 0 {
			return fmt.Errorf("workflow definition is required")
		}
		return nil
	default:
		return fmt.Errorf("unsupported plugin kind %q", kind)
	}
}

func validateMCPDefinition(value MCPDefinition) error {
	switch value.Transport {
	case "http", "sse":
		if strings.TrimSpace(value.URL) == "" {
			return fmt.Errorf("mcp remote definition requires url")
		}
	case "stdio":
		if err := validateMCPStdioDefinition(value); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported mcp transport %q", value.Transport)
	}
	return nil
}

func validateConnectorDefinition(raw json.RawMessage) error {
	var value ConnectorDefinition
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value.Schema != "connector/v1" {
		return fmt.Errorf("unsupported connector definition schema %q", value.Schema)
	}
	if strings.TrimSpace(value.Channel) == "" {
		return fmt.Errorf("connector channel is required")
	}
	hasSkill, hasMCP := value.Skill != nil, value.MCP != nil
	switch value.Mode {
	case ConnectorModeSkillOnly:
		if !hasSkill || hasMCP {
			return fmt.Errorf("skill_only connector requires only a skill")
		}
	case ConnectorModeMCPOnly:
		if hasSkill || !hasMCP {
			return fmt.Errorf("mcp_only connector requires only an mcp")
		}
	case ConnectorModeHybrid:
		if !hasSkill || !hasMCP {
			return fmt.Errorf("hybrid connector requires a skill and an mcp")
		}
	default:
		return fmt.Errorf("unsupported connector mode %q", value.Mode)
	}
	if hasSkill {
		if strings.TrimSpace(value.Skill.Code) == "" || value.Skill.Revision <= 0 ||
			value.Skill.Artifact == nil || strings.TrimSpace(value.Skill.Artifact.FileUploadID) == "" ||
			strings.TrimSpace(value.Skill.Artifact.SHA256) == "" {
			return fmt.Errorf("connector skill artifact is incomplete")
		}
	}
	if hasMCP {
		if value.MCP.Schema != "mcp/v1" {
			return fmt.Errorf("connector mcp schema must be mcp/v1")
		}
		if err := validateMCPDefinition(*value.MCP); err != nil {
			return err
		}
	}
	if value.Auth.Type == types.MCPChannelAuthTypeOAuth {
		if value.Auth.OAuth == nil {
			return fmt.Errorf("oauth connector requires oauth state")
		}
		switch value.Auth.OAuth.Status {
		case ConnectorOAuthPending, ConnectorOAuthExchanging, ConnectorOAuthActive,
			ConnectorOAuthFailed, ConnectorOAuthReauthorizationRequired:
		default:
			return fmt.Errorf("unsupported connector oauth status %q", value.Auth.OAuth.Status)
		}
	}
	for envName, valueKey := range value.Auth.Bindings.SkillEnv {
		if !envNamePattern.MatchString(envName) || strings.TrimSpace(valueKey) == "" {
			return fmt.Errorf("connector skill environment binding is invalid")
		}
	}
	return nil
}

func validateMCPStdioDefinition(value MCPDefinition) error {
	if strings.TrimSpace(value.Command) == "" || strings.ContainsRune(value.Command, '\x00') {
		return fmt.Errorf("mcp stdio definition requires a command without NUL")
	}
	for _, arg := range value.Args {
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("mcp stdio definition argument cannot contain NUL")
		}
	}
	for name, envValue := range value.Env {
		if !envNamePattern.MatchString(name) || strings.ContainsRune(envValue, '\x00') {
			return fmt.Errorf("mcp stdio definition environment is invalid")
		}
	}
	return nil
}

// MCPFromDefinition decodes one validated MCP revision definition.
func MCPFromDefinition(raw json.RawMessage) (*MCPDefinition, error) {
	if err := ValidatePluginDefinition("mcp", raw); err != nil {
		return nil, err
	}
	var envelope struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Schema == "connector/v1" {
		value, err := ConnectorFromDefinition(raw)
		if err != nil {
			return nil, err
		}
		if value.MCP == nil {
			return nil, nil
		}
		mcp := *value.MCP
		mcp.Headers = cloneStringMap(value.MCP.Headers)
		mcp.Env = cloneStringMap(value.MCP.Env)
		if key := strings.TrimSpace(value.Auth.Bindings.MCPBearerToken); key != "" {
			if credential := value.Auth.Values[key]; credential != "" {
				mcp.BearerToken = credential
			}
		}
		for header, key := range value.Auth.Bindings.MCPHeaders {
			if credential := value.Auth.Values[key]; credential != "" {
				if mcp.Headers == nil {
					mcp.Headers = make(map[string]string)
				}
				mcp.Headers[header] = credential
			}
		}
		for envName, key := range value.Auth.Bindings.MCPEnv {
			if credential := value.Auth.Values[key]; credential != "" {
				if mcp.Env == nil {
					mcp.Env = make(map[string]string)
				}
				mcp.Env[envName] = credential
			}
		}
		if value.Auth.Type == types.MCPChannelAuthTypeOAuth &&
			(value.Auth.OAuth == nil || value.Auth.OAuth.Status != ConnectorOAuthActive) {
			return nil, nil
		}
		if len(value.Auth.Bindings.MCPQuery) > 0 {
			parsed, parseErr := url.Parse(mcp.URL)
			if parseErr != nil {
				return nil, parseErr
			}
			query := parsed.Query()
			for queryName, key := range value.Auth.Bindings.MCPQuery {
				if credential := value.Auth.Values[key]; credential != "" {
					query.Set(queryName, credential)
				}
			}
			parsed.RawQuery = query.Encode()
			mcp.URL = parsed.String()
		}
		return &mcp, nil
	}
	var value MCPDefinition
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

// ConnectorFromDefinition decodes a connector revision, returning nil for legacy mcp/v1 definitions.
func ConnectorFromDefinition(raw json.RawMessage) (*ConnectorDefinition, error) {
	var envelope struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Schema != "connector/v1" {
		return nil, nil
	}
	if err := ValidatePluginDefinition("mcp", raw); err != nil {
		return nil, err
	}
	var value ConnectorDefinition
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

// ArtifactFromDefinition obtains the bundle metadata used by worker download code.
func ArtifactFromDefinition(kind string, raw json.RawMessage) (*ArtifactDefinition, error) {
	if strings.ToLower(kind) != "skill" {
		return nil, nil
	}
	if err := ValidatePluginDefinition(kind, raw); err != nil {
		return nil, err
	}
	var value skillDefinition
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value.Artifact, nil
}

func isBundleDefinition(kind string) bool { return strings.EqualFold(strings.TrimSpace(kind), "skill") }
