import { apiClient } from "./client";
import type { SkillMarketplaceItem } from "./pluginDisplayTypes";
import type { BackendDataResponse } from "./types";

export interface PluginListItem {
	public_id: string;
	code: string;
	kind: string;
	name: string;
	display_name?: string;
	description?: string;
	status: string;
	origin: string;
	current_revision: number;
	visibility?: PluginVisibility;
	permission?: PluginPermission;
}

export type PluginVisibility = "public" | "private";
export type PluginPermissionRole = "owner" | "admin" | "viewer";

export interface PluginPermission {
	role: PluginPermissionRole;
}

export interface PluginPermissionUser {
	public_id: string;
	name: string;
	email?: string;
	avatar_url?: string;
	departments?: Array<{ department_id: number; name: string }>;
}

export interface PluginPermissionMember {
	user: PluginPermissionUser;
	role: PluginPermissionRole;
}

export interface PluginPermissionSettings {
	visibility: PluginVisibility;
	members: PluginPermissionMember[];
}

export interface ListPluginsResponse {
	plugins: PluginListItem[];
}

export interface GetPluginResponse {
	plugin: PluginListItem;
	content: PluginRevisionContent | null;
	definition?: MCPPluginDefinition | ConnectorPluginDefinition;
}

export type MCPTransport = "http" | "stdio";

export interface MCPPluginDefinition {
	schema: "mcp/v1";
	transport: MCPTransport;
	name: string;
	provider?: string;
	url?: string;
	bearer_token?: string;
	headers?: Record<string, string>;
	command?: string;
	args?: string[];
	env?: Record<string, string>;
}

export interface ConnectorPluginDefinition {
	schema: "connector/v1";
	channel: string;
	mode: "skill_only" | "mcp_only" | "hybrid";
}

export interface MCPPluginConfig {
	code?: string;
	name: string;
	description?: string;
	transport: MCPTransport;
	url?: string;
	bearer_token?: string;
	headers?: Record<string, string>;
	command?: string;
	args?: string[];
	env?: Record<string, string>;
}

export interface TestMCPPluginParams {
	transport?: "http";
	url: string;
	bearer_token?: string;
	headers?: Record<string, string>;
}

export interface TestMCPPluginResponse {
	ok: boolean;
	tool_count: number;
}

export interface MCPPlatform {
	code: string;
	name: string;
	description: string;
	mode: "skill_only" | "mcp_only" | "hybrid";
	auth_type: "none" | "form" | "oauth" | "managed";
	auth_description?: string;
	auth_fields?: MCPPlatformAuthField[];
	auto_connect_supported: boolean;
	connected: boolean;
	authorization_status?:
		| "disconnected"
		| "pending"
		| "exchanging"
		| "active"
		| "failed"
		| "reauthorization_required";
	plugin_id?: string;
}

export interface MCPPlatformAuthField {
	key: string;
	label: string;
	type: "text" | "password";
	required: boolean;
	placeholder?: string;
	description?: string;
}

export interface ConnectMCPPlatformParams {
	auth_values?: Record<string, string>;
}

export interface ListMCPPlatformsResponse {
	platforms: MCPPlatform[];
}

export interface ConnectMCPPlatformResponse {
	platform: MCPPlatform;
	plugin: PluginListItem;
	tool_count: number;
}

export interface StartMCPPlatformOAuthResponse {
	attempt_id: string;
	authorization_url: string;
	expires_at: string;
}

export interface MCPPlatformOAuthStatusResponse {
	attempt_id: string;
	status: NonNullable<MCPPlatform["authorization_status"]>;
	plugin_id?: string;
	display_name?: string;
	error_code?: string;
	connected: boolean;
}

export interface GetPluginInstallationStatusParams {
	kind: string;
	code: string;
}

export interface PluginInstallationStatus {
	kind: string;
	code: string;
	installed: boolean;
	plugin_id?: string;
	current_version?: string;
	marketplace_based: boolean;
	marketplace_item_id?: string;
	installed_marketplace_version?: string;
	marketplace_available: boolean;
	latest_marketplace_version?: string;
	update_available: boolean;
}

export interface PluginRevisionFile {
	path: string;
	size_bytes: number;
	sha256: string;
}

export interface PluginRevisionContent {
	schema: string;
	version: number;
	entrypoint_path: string;
	skill_md: string;
	files: PluginRevisionFile[];
}

export interface DeletePluginResponse {
	operation: "deleted" | "project_unbound" | string;
}

export interface ProjectPluginItem extends PluginListItem {}

export interface ListProjectPluginsParams {
	public_id: string;
	kind?: string;
}

export interface UpdateProjectPluginParams {
	public_id: string;
	plugin_id: string;
}

export interface ListPluginsParams {
	kind?: string;
	status?: string;
	keyword?: string;
	relation?: "owner" | "admin" | "viewer" | "shared";
	limit?: number;
	exclude_marketplace_based?: boolean;
}

export interface AddSkillPluginParams {
	mode: "file" | "github";
	file_upload_id?: string;
	github_url?: string;
}

export interface AddSkillPluginResponse {
	operation: "created" | "updated" | "unchanged";
	plugin: PluginListItem;
}

function cleanListParams(params: ListPluginsParams): Record<string, string | number> {
	const result: Record<string, string | number> = {};
	if (params.kind) result.kind = params.kind;
	if (params.status) result.status = params.status;
	if (params.keyword) result.keyword = params.keyword;
	if (params.relation) result.relation = params.relation;
	if (params.limit !== undefined) result.limit = params.limit;
	if (params.exclude_marketplace_based !== undefined) {
		result.exclude_marketplace_based = params.exclude_marketplace_based ? 1 : 0;
	}
	return result;
}

// pluginToSkillCard maps organization plugins to the existing Skill card presentation.
export function pluginToSkillCard(plugin: PluginListItem): SkillMarketplaceItem {
	return {
		source_type: "organization",
		skill_id: plugin.public_id,
		name: plugin.code,
		display_name: plugin.display_name || plugin.name,
		description: plugin.description ?? "",
		version: `r${plugin.current_revision}`,
		author: "组织插件",
		category: plugin.kind,
		tags: [plugin.kind],
		icon: "",
		installs: 0,
		verified: false,
		visibility: plugin.visibility,
		permission: plugin.permission,
	};
}

export interface PluginComposerOption {
	code: string;
	label: string;
	description: string;
	keywords: string[];
	source?: "project" | "organization" | "marketplace" | "builtin";
	origin?: string;
	pluginId?: string;
	marketplaceItemId?: string;
	projectAssociated?: boolean;
	organizationOverride?: boolean;
}

// pluginToComposerOption maps PluginListItem (or ProjectPluginItem) to a composer picker option.
export function pluginToComposerOption(
	plugin: PluginListItem,
	source?: "project" | "organization" | "builtin",
): PluginComposerOption {
	const resolvedSource =
		source ?? (plugin.origin === "builtin_worker" ? "builtin" : "organization");
	return {
		code: plugin.code,
		label: plugin.display_name || plugin.name || plugin.code,
		description: plugin.description ?? "",
		source: resolvedSource,
		origin: plugin.origin,
		pluginId: resolvedSource === "builtin" ? undefined : plugin.public_id,
		projectAssociated: resolvedSource === "project",
		keywords: [plugin.name, plugin.display_name, plugin.code, plugin.description].filter(
			(item): item is string => Boolean(item),
		),
	};
}

/**
 * Merges every Skill source into one ordered list.
 * Order: project-bound > organization > marketplace > system builtin.
 * Duplicate codes are matched case-insensitively; display labels do not affect identity.
 */
export function mergeSkillOptions(
	projectSkills: PluginComposerOption[],
	orgSkills: PluginComposerOption[],
	marketplaceSkills: PluginComposerOption[],
	builtinSkills: PluginComposerOption[],
): PluginComposerOption[] {
	const seenCodes = new Set<string>();
	const result: PluginComposerOption[] = [];

	const add = (skill: PluginComposerOption) => {
		const codeKey = skill.code.toLowerCase();
		if (seenCodes.has(codeKey)) return;
		seenCodes.add(codeKey);
		result.push(skill);
	};

	for (const skill of projectSkills) add(skill);
	for (const skill of orgSkills) add(skill);
	for (const skill of marketplaceSkills) add(skill);
	for (const skill of builtinSkills) add(skill);

	return result;
}

export const pluginApi = {
	list: (params: ListPluginsParams = {}) =>
		apiClient.get<BackendDataResponse<ListPluginsResponse>>("/plugins", {
			params: cleanListParams(params),
		}),
	get: (pluginID: string) =>
		apiClient.get<BackendDataResponse<GetPluginResponse>>(`/plugins/${pluginID}`),
	getPermissions: (pluginID: string) =>
		apiClient.get<BackendDataResponse<PluginPermissionSettings>>(
			`/plugins/${pluginID}/permissions`,
		),
	updatePermissions: (pluginID: string, params: PluginPermissionSettings) =>
		apiClient.put<BackendDataResponse<PluginPermissionSettings>>(
			`/plugins/${pluginID}/permissions`,
			params,
		),
	getInstallationStatus: (params: GetPluginInstallationStatusParams) =>
		apiClient.get<BackendDataResponse<PluginInstallationStatus>>("/plugins/installation-status", {
			params: { kind: params.kind, code: params.code },
		}),
	delete: (pluginID: string) =>
		apiClient.delete<BackendDataResponse<DeletePluginResponse>>(`/plugins/${pluginID}`),
	addSkill: (params: AddSkillPluginParams) =>
		apiClient.post<BackendDataResponse<AddSkillPluginResponse>>("/plugins/skills", params),
	addMCP: (params: MCPPluginConfig) =>
		apiClient.post<BackendDataResponse<PluginListItem>>("/plugins/mcp", params),
	updateMCP: (pluginID: string, params: MCPPluginConfig) =>
		apiClient.put<BackendDataResponse<PluginListItem>>(`/plugins/mcp/${pluginID}`, params),
	testMCP: (params: TestMCPPluginParams) =>
		apiClient.post<BackendDataResponse<TestMCPPluginResponse>>("/plugins/mcp/test", params),
	listMCPPlatforms: () =>
		apiClient.get<BackendDataResponse<ListMCPPlatformsResponse>>("/plugins/mcp/platforms"),
	connectMCPPlatform: (platformCode: string, params: ConnectMCPPlatformParams = {}) =>
		apiClient.post<BackendDataResponse<ConnectMCPPlatformResponse>>(
			`/plugins/mcp/platforms/${platformCode}/connect`,
			params,
		),
	testMCPPlatform: (platformCode: string) =>
		apiClient.post<BackendDataResponse<TestMCPPluginResponse>>(
			`/plugins/mcp/platforms/${platformCode}/test`,
		),
	startMCPPlatformOAuth: (platformCode: string) =>
		apiClient.post<BackendDataResponse<StartMCPPlatformOAuthResponse>>(
			`/plugins/mcp/platforms/${platformCode}/oauth/start`,
		),
	getMCPPlatformOAuthStatus: (platformCode: string, attemptID: string) =>
		apiClient.get<BackendDataResponse<MCPPlatformOAuthStatusResponse>>(
			`/plugins/mcp/platforms/${platformCode}/oauth/status`,
			{ params: { attempt_id: attemptID } },
		),
	listProject: (params: ListProjectPluginsParams) =>
		apiClient.post<BackendDataResponse<ProjectPluginItem[]>>("/ListProjectPlugins", params),
	addToProject: (params: UpdateProjectPluginParams) =>
		apiClient.post<BackendDataResponse<null>>("/AddProjectPlugin", params),
	removeFromProject: (params: UpdateProjectPluginParams) =>
		apiClient.post<BackendDataResponse<null>>("/RemoveProjectPlugin", params),
	listBuiltinSkills: () =>
		apiClient.get<BackendDataResponse<ListPluginsResponse>>("/plugins/builtin-skills"),
};
