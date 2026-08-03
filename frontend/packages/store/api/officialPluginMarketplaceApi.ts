import { apiClient } from "./client";
import type { PluginRevisionContent } from "./pluginApi";
import type { BackendDataResponse } from "./types";

export interface OfficialPluginMarketplaceItem {
	public_id: string;
	code: string;
	kind: string;
	name: string;
	description?: string;
	author: string;
	version: string;
	category: string;
	tags: string[];
	icon?: string;
	verified: boolean;
	installed: boolean;
	installed_plugin_id?: string;
	marketplace_available: boolean;
	latest_version?: string;
	update_available: boolean;
	organization_override: boolean;
	content?: PluginRevisionContent | null;
}

export interface ListOfficialPluginMarketplaceItemsParams {
	kind?: string;
	category?: string;
	keyword?: string;
	limit?: number;
}

export interface ListOfficialPluginMarketplaceItemsResponse {
	items: OfficialPluginMarketplaceItem[];
}

export interface GetOfficialPluginLatestVersionParams {
	kind: string;
	code: string;
}

export interface OfficialPluginLatestVersion {
	kind: string;
	code: string;
	available: boolean;
	item_id?: string;
	latest_version?: string;
}

export interface InstallOfficialPluginResponse {
	operation: "installed" | "updated" | "already_current";
	plugin: {
		public_id: string;
		code: string;
		kind: string;
		name: string;
		description?: string;
		status: string;
		origin: string;
		current_revision: number;
	};
}

function cleanParams(
	params: ListOfficialPluginMarketplaceItemsParams,
): Record<string, string | number> {
	const result: Record<string, string | number> = {};
	if (params.kind) result.kind = params.kind;
	if (params.category) result.category = params.category;
	if (params.keyword) result.keyword = params.keyword;
	if (params.limit !== undefined) result.limit = params.limit;
	return result;
}

export const officialPluginMarketplaceApi = {
	list: (params: ListOfficialPluginMarketplaceItemsParams = {}) =>
		apiClient.get<BackendDataResponse<ListOfficialPluginMarketplaceItemsResponse>>(
			"/plugin-marketplace/items",
			{ params: cleanParams(params) },
		),
	get: (itemID: string) =>
		apiClient.get<BackendDataResponse<OfficialPluginMarketplaceItem>>(
			`/plugin-marketplace/items/${itemID}`,
		),
	getLatestVersion: (params: GetOfficialPluginLatestVersionParams) =>
		apiClient.get<BackendDataResponse<OfficialPluginLatestVersion>>(
			"/plugin-marketplace/items/latest-version",
			{ params: { kind: params.kind, code: params.code } },
		),
	install: (itemID: string) =>
		apiClient.post<BackendDataResponse<InstallOfficialPluginResponse>>(
			`/plugin-marketplace/items/${itemID}/install`,
		),
};
