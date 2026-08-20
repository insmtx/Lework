package contract

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

var (
	// ErrPluginNotFound indicates a plugin resource is not visible in the requested scope.
	ErrPluginNotFound = errors.New("plugin not found")
	// ErrPluginForbidden indicates a plugin is visible but the caller lacks the requested permission.
	ErrPluginForbidden = errors.New("permission denied")
	// ErrPluginImportNotImplemented indicates that package publication is deferred to a later phase.
	ErrPluginImportNotImplemented = errors.New("plugin import is not implemented")
	// ErrInvalidPluginConfig indicates that a plugin configuration failed validation.
	ErrInvalidPluginConfig = errors.New("invalid plugin config")
	// ErrPluginPermissionUnsupported indicates the plugin type does not support sharing configuration.
	ErrPluginPermissionUnsupported = errors.New("plugin permission configuration is not supported for this plugin type")
	// ErrInvalidPluginPermission indicates an invalid permission update request.
	ErrInvalidPluginPermission = errors.New("invalid plugin permission request")
)

const (
	PluginScopeOrganization = "organization"
)

// ListPluginsRequest describes filters for organization plugin lists.
type ListPluginsRequest struct {
	Kind                    string `form:"kind" json:"kind,omitempty"`
	Status                  string `form:"status" json:"status,omitempty"`
	Category                string `form:"category" json:"category,omitempty"`
	Keyword                 string `form:"keyword" json:"keyword,omitempty"`
	Offset                  int    `form:"offset" json:"offset,omitempty"`
	Limit                   int    `form:"limit" json:"limit,omitempty"`
	Relation                string `form:"relation" json:"relation,omitempty"`
	ExcludeMarketplaceBased bool   `form:"exclude_marketplace_based" json:"exclude_marketplace_based,omitempty"`
}

// PluginPermission is the caller's direct role on one plugin.
type PluginPermission struct {
	Role types.ResourceRole `json:"role,omitempty"`
}

// PluginView is the safe API representation of an organization plugin.
type PluginView struct {
	PublicID        string            `json:"public_id"`
	Code            string            `json:"code"`
	Kind            string            `json:"kind"`
	Name            string            `json:"name"`
	DisplayName     string            `json:"display_name,omitempty"`
	Description     string            `json:"description,omitempty"`
	Visibility      string            `json:"visibility,omitempty"`
	Permission      *PluginPermission `json:"permission,omitempty"`
	Status          string            `json:"status"`
	Origin          string            `json:"origin"`
	CurrentRevision int               `json:"current_revision"`
}

// ListPluginsResponse contains organization plugins.
type ListPluginsResponse struct {
	Plugins []PluginView `json:"plugins"`
}

// GetPluginRequest selects an organization plugin by public ID.
type GetPluginRequest struct{}

// GetPluginResponse contains one organization plugin detail.
type GetPluginResponse struct {
	Plugin  *PluginView                `json:"plugin,omitempty"`
	Content *PluginRevisionContentView `json:"content"`
	// Definition is returned for non-bundle plugin kinds such as MCP.
	Definition json.RawMessage `json:"definition,omitempty"`
}

// GetPluginInstallationStatusRequest identifies one organization plugin by stable identity.
type GetPluginInstallationStatusRequest struct {
	Kind string `form:"kind" json:"kind"`
	Code string `form:"code" json:"code"`
}

// PluginInstallationStatusResponse describes installation and official update state.
type PluginInstallationStatusResponse struct {
	Kind                        string `json:"kind"`
	Code                        string `json:"code"`
	Installed                   bool   `json:"installed"`
	PluginID                    string `json:"plugin_id,omitempty"`
	CurrentVersion              string `json:"current_version,omitempty"`
	MarketplaceBased            bool   `json:"marketplace_based"`
	MarketplaceItemID           string `json:"marketplace_item_id,omitempty"`
	InstalledMarketplaceVersion string `json:"installed_marketplace_version,omitempty"`
	MarketplaceAvailable        bool   `json:"marketplace_available"`
	LatestMarketplaceVersion    string `json:"latest_marketplace_version,omitempty"`
	UpdateAvailable             bool   `json:"update_available"`
}

// PluginRevisionFileView exposes one immutable file in the current plugin revision.
type PluginRevisionFileView struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// PluginRevisionContentView contains the current revision content used by detail pages.
type PluginRevisionContentView struct {
	Schema         string                   `json:"schema"`
	Version        int                      `json:"version"`
	EntrypointPath string                   `json:"entrypoint_path"`
	SkillMD        string                   `json:"skill_md"`
	Files          []PluginRevisionFileView `json:"files"`
}

// PluginRevisionView exposes immutable revision metadata without the storage URI.
type PluginRevisionView struct {
	Revision        int       `json:"revision"`
	Status          string    `json:"status"`
	PublishedByType string    `json:"published_by_type"`
	PublishedByID   uint      `json:"published_by_id"`
	PublishedAt     time.Time `json:"published_at"`
}

// ListPluginVersionsResponse contains organization plugin revisions.
type ListPluginVersionsResponse struct {
	Versions []PluginRevisionView `json:"versions"`
}

// DeletePluginRequest optionally removes a plugin from one project instead of archiving it.
type DeletePluginRequest struct {
	ProjectID string `form:"project_id" json:"project_id,omitempty"`
}

// DeletePluginResponse reports which operation completed.
type DeletePluginResponse struct {
	Operation string `json:"operation"`
}

const (
	SkillAddModeFile   = "file"
	SkillAddModeGitHub = "github"
)

// AddSkillPluginRequest adds a Skill plugin using the selected source mode.
type AddSkillPluginRequest struct {
	Mode         string `json:"mode"`
	FileUploadID string `json:"file_upload_id"`
	GitHubURL    string `json:"github_url"`
}

// AddSkillPluginResponse reports the result of importing or publishing a Skill.
type AddSkillPluginResponse struct {
	Operation string     `json:"operation"`
	Plugin    PluginView `json:"plugin"`
}

// MCPPluginConfig is the organization-managed HTTP or stdio MCP configuration.
// Code is optional on create and update; the service generates it for new plugins.
type MCPPluginConfig struct {
	Code        string            `json:"code,omitempty"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Transport   string            `json:"transport,omitempty"`
	URL         string            `json:"url,omitempty"`
	BearerToken string            `json:"bearer_token,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Provider    string            `json:"-"`
}

// AddMCPPluginRequest creates one organization MCP plugin.
type AddMCPPluginRequest struct {
	MCPPluginConfig
}

// UpdateMCPPluginRequest replaces one organization MCP configuration.
type UpdateMCPPluginRequest struct {
	MCPPluginConfig
}

// TestMCPPluginRequest tests a draft remote HTTP MCP configuration without storing it.
type TestMCPPluginRequest struct {
	Transport   string            `json:"transport,omitempty"`
	URL         string            `json:"url"`
	BearerToken string            `json:"bearer_token,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// TestMCPPluginResponse contains the result of one stateless MCP handshake.
type TestMCPPluginResponse struct {
	OK        bool `json:"ok"`
	ToolCount int  `json:"tool_count"`
}

// MCPPlatformView describes one built-in platform and the caller's connection state.
type MCPPlatformView struct {
	Code                 string                      `json:"code"`
	Name                 string                      `json:"name"`
	Description          string                      `json:"description"`
	Mode                 string                      `json:"mode"`
	AuthType             string                      `json:"auth_type"`
	AuthDescription      string                      `json:"auth_description,omitempty"`
	AuthFields           []types.MCPChannelAuthField `json:"auth_fields,omitempty"`
	AutoConnectSupported bool                        `json:"auto_connect_supported"`
	Connected            bool                        `json:"connected"`
	AuthorizationStatus  string                      `json:"authorization_status,omitempty"`
	PluginID             string                      `json:"plugin_id,omitempty"`
}

// ListMCPPlatformsResponse contains the built-in MCP platform catalogue.
type ListMCPPlatformsResponse struct {
	Platforms []MCPPlatformView `json:"platforms"`
}

// ConnectMCPPlatformResponse returns the connected platform and created plugin.
type ConnectMCPPlatformResponse struct {
	Platform  MCPPlatformView `json:"platform"`
	Plugin    PluginView      `json:"plugin"`
	ToolCount int             `json:"tool_count"`
}

// ConnectMCPPlatformRequest contains schema-defined authorization values.
type ConnectMCPPlatformRequest struct {
	AuthValues map[string]string `json:"auth_values,omitempty"`
}

// StartMCPPlatformOAuthResponse starts one persisted OAuth authorization attempt.
type StartMCPPlatformOAuthResponse struct {
	AttemptID        string    `json:"attempt_id"`
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// MCPPlatformOAuthStatusResponse is the safe polling view of one OAuth attempt.
type MCPPlatformOAuthStatusResponse struct {
	AttemptID   string `json:"attempt_id"`
	Status      string `json:"status"`
	PluginID    string `json:"plugin_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	ErrorCode   string `json:"error_code,omitempty"`
	Connected   bool   `json:"connected"`
}

// ConnectorSkillRef identifies the immutable project connector revision requesting a Skill artifact.
type ConnectorSkillRef struct {
	PluginID string `json:"plugin_id"`
	Revision int    `json:"revision"`
}

// ResolveSkillDownloadURLsRequest selects the current downloadable artifacts by Skill code.
// Codes that are unavailable to the caller are omitted from the response.
type ResolveSkillDownloadURLsRequest struct {
	SkillCodes      []string            `json:"skill_codes"`
	ConnectorSkills []ConnectorSkillRef `json:"connector_skills,omitempty"`
	// ActorUin 标识任务执行的实际用户，避免把 Worker 身份误当成插件 owner。
	ActorUin uint `json:"actor_uin,omitempty"`
	// ProjectID 是任务运行所在项目，存在有效项目绑定时按项目运行授权允许下载。
	ProjectID string `json:"project_id,omitempty"`
}

// SkillDownloadURL is the worker-safe projection of one current Skill artifact.
type SkillDownloadURL struct {
	Code        string `json:"code"`
	Revision    int    `json:"revision"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"download_url"`
}

// OfficialPluginMarketplaceItemView is the public projection of one official plugin.
type OfficialPluginMarketplaceItemView struct {
	PublicID             string                     `json:"public_id"`
	Code                 string                     `json:"code"`
	Kind                 string                     `json:"kind"`
	Name                 string                     `json:"name"`
	DisplayName          string                     `json:"display_name,omitempty"`
	Description          string                     `json:"description,omitempty"`
	Author               string                     `json:"author"`
	Version              string                     `json:"version"`
	Category             string                     `json:"category"`
	Tags                 []string                   `json:"tags"`
	Icon                 string                     `json:"icon,omitempty"`
	Verified             bool                       `json:"verified"`
	Installed            bool                       `json:"installed"`
	InstalledPluginID    string                     `json:"installed_plugin_id,omitempty"`
	MarketplaceAvailable bool                       `json:"marketplace_available"`
	LatestVersion        string                     `json:"latest_version,omitempty"`
	UpdateAvailable      bool                       `json:"update_available"`
	OrganizationOverride bool                       `json:"organization_override"`
	Content              *PluginRevisionContentView `json:"content,omitempty"`
}

// ListOfficialPluginMarketplaceItemsRequest filters the official plugin catalogue.
type ListOfficialPluginMarketplaceItemsRequest struct {
	Kind     string `form:"kind" json:"kind,omitempty"`
	Category string `form:"category" json:"category,omitempty"`
	Keyword  string `form:"keyword" json:"keyword,omitempty"`
	Limit    int    `form:"limit" json:"limit,omitempty"`
}

type ListOfficialPluginMarketplaceItemsResponse struct {
	Items []OfficialPluginMarketplaceItemView `json:"items"`
}

// GetOfficialPluginLatestVersionRequest identifies one official plugin by stable identity.
type GetOfficialPluginLatestVersionRequest struct {
	Kind string `form:"kind" json:"kind"`
	Code string `form:"code" json:"code"`
}

// OfficialPluginLatestVersionResponse reports whether an official release is available.
type OfficialPluginLatestVersionResponse struct {
	Kind          string `json:"kind"`
	Code          string `json:"code"`
	Available     bool   `json:"available"`
	ItemID        string `json:"item_id,omitempty"`
	LatestVersion string `json:"latest_version,omitempty"`
}

type InstallOfficialPluginResponse struct {
	Operation string     `json:"operation"`
	Plugin    PluginView `json:"plugin"`
}

// PluginPermissionUserView 是权限接口返回的公开用户展示信息。
type PluginPermissionUserView struct {
	PublicID    string                       `json:"public_id"`
	Name        string                       `json:"name"`
	Email       string                       `json:"email,omitempty"`
	AvatarURL   string                       `json:"avatar_url,omitempty"`
	Departments []PluginPermissionDepartment `json:"departments,omitempty"`
}

// PluginPermissionDepartment 是成员部门展示信息。
type PluginPermissionDepartment struct {
	DepartmentID uint   `json:"department_id"`
	Name         string `json:"name"`
}

// PluginPermissionMemberView 是权限接口返回的成员展示信息。
type PluginPermissionMemberView struct {
	User PluginPermissionUserView `json:"user"`
	Role types.ResourceRole       `json:"role"`
}

// PluginPermissionSettingsView 是权限读取/更新接口的响应。
type PluginPermissionSettingsView struct {
	Visibility types.PluginVisibility       `json:"visibility"`
	Members    []PluginPermissionMemberView `json:"members"`
}

// PluginPermissionMemberInput 是权限更新接口写入的成员身份。
// 身份标识为 user.public_id，展示字段由服务端重新解析，不信任写入值。
type PluginPermissionMemberInput struct {
	User struct {
		PublicID string `json:"public_id"`
	} `json:"user"`
	Role types.ResourceRole `json:"role"`
}

// UpdatePluginPermissionsRequest 全量替换插件权限配置。
type UpdatePluginPermissionsRequest struct {
	Visibility types.PluginVisibility        `json:"visibility"`
	Members    []PluginPermissionMemberInput `json:"members"`
}

// OfficialPluginMarketplaceService isolates official catalogue reads and installs from organization plugin APIs.
type OfficialPluginMarketplaceService interface {
	ListOfficialPluginMarketplaceItems(ctx context.Context, orgID uint, req *ListOfficialPluginMarketplaceItemsRequest) (*ListOfficialPluginMarketplaceItemsResponse, error)
	GetOfficialPluginMarketplaceItem(ctx context.Context, orgID uint, itemID string) (*OfficialPluginMarketplaceItemView, error)
	GetOfficialPluginLatestVersion(ctx context.Context, req *GetOfficialPluginLatestVersionRequest) (*OfficialPluginLatestVersionResponse, error)
	InstallOfficialPlugin(ctx context.Context, orgID, uin uint, itemID string) (*InstallOfficialPluginResponse, error)
}

// ResolveSkillDownloadURLsResponse contains only the Skill artifacts that could be resolved.
type ResolveSkillDownloadURLsResponse struct {
	Skills []SkillDownloadURL `json:"skills"`
}

// PluginService defines the new organization plugin management contract.
type PluginService interface {
	ListPlugins(ctx context.Context, orgID, uin uint, req *ListPluginsRequest) (*ListPluginsResponse, error)
	GetPlugin(ctx context.Context, orgID, uin uint, pluginID string, req *GetPluginRequest) (*GetPluginResponse, error)
	GetPluginInstallationStatus(ctx context.Context, orgID, uin uint, req *GetPluginInstallationStatusRequest) (*PluginInstallationStatusResponse, error)
	ListPluginVersions(ctx context.Context, orgID, uin uint, pluginID string) (*ListPluginVersionsResponse, error)
	DeletePlugin(ctx context.Context, orgID, uin uint, pluginID string, req *DeletePluginRequest) (*DeletePluginResponse, error)
	GetPluginPermissions(ctx context.Context, orgID, uin uint, pluginID string) (*PluginPermissionSettingsView, error)
	UpdatePluginPermissions(ctx context.Context, orgID, uin uint, pluginID string, req *UpdatePluginPermissionsRequest) (*PluginPermissionSettingsView, error)
	AddSkillPlugin(ctx context.Context, orgID, uin uint, req *AddSkillPluginRequest) (*AddSkillPluginResponse, error)
	AddMCPPlugin(ctx context.Context, orgID, uin uint, req *AddMCPPluginRequest) (*PluginView, error)
	UpdateMCPPlugin(ctx context.Context, orgID, uin uint, pluginID string, req *UpdateMCPPluginRequest) (*PluginView, error)
	TestMCPPlugin(ctx context.Context, req *TestMCPPluginRequest) (*TestMCPPluginResponse, error)
	ListMCPPlatforms(ctx context.Context, orgID, uin uint) (*ListMCPPlatformsResponse, error)
	ConnectMCPPlatform(ctx context.Context, orgID, uin uint, platformCode string, req *ConnectMCPPlatformRequest) (*ConnectMCPPlatformResponse, error)
	TestMCPPlatform(ctx context.Context, orgID, uin uint, platformCode string) (*TestMCPPluginResponse, error)
	StartMCPPlatformOAuth(ctx context.Context, orgID, uin uint, platformCode string) (*StartMCPPlatformOAuthResponse, error)
	GetMCPPlatformOAuthStatus(ctx context.Context, orgID, uin uint, platformCode, attemptID string) (*MCPPlatformOAuthStatusResponse, error)
	CompleteMCPPlatformOAuth(ctx context.Context, platformCode, state, code, providerError string) (*MCPPlatformOAuthStatusResponse, error)
	ResolveSkillDownloadURLs(ctx context.Context, orgID uint, callerKind types.CallerKind, callerID uint, req *ResolveSkillDownloadURLsRequest) (*ResolveSkillDownloadURLsResponse, error)
	ListBuiltinSkills(ctx context.Context) (*ListPluginsResponse, error)
}
