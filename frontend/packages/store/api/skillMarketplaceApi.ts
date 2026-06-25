import {apiClient} from "./client";
import type {BackendDataResponse} from "./types";

export interface SkillMarketplaceItem {
  source_type: string;
  skill_id: string;
  name: string;
  description: string;
  version: string;
  author: string;
  category: string;
  tags: string[] | null;
  icon: string;
  installs: number;
}

export interface SearchSkillMarketplaceResponse {
  items: SkillMarketplaceItem[];
  warnings?: Array<{ source_type: string; message: string }>;
}

export interface SearchSkillMarketplaceParams {
  keyword?: string;
  category?: string;
  source_types?: string[];
  limit?: number;
}

export interface InstallSkillParams {
  source: string;
  skill_id: string;
  version?: string;
}

export interface InstallSkillResponse {
  status: string;
  message: string;
}

/** 后端返回的已安装 skill（worker 侧查询结果）。 */
export interface SkillInstalledItem {
  name: string;
  description: string;
  category: string;
  source: string;
  trust: string;
}

export interface InstalledSkillsResponse {
  skills: SkillInstalledItem[];
}

export interface UninstallSkillParams {
  name: string;
}

export interface UninstallSkillResponse {
  status: string;
  message: string;
}

export interface SkillDetailParams {
  source: string;
  skill_id: string;
  version?: string;
}

export interface SkillDetailData {
  skill_id: string;
  source: string;
  name: string;
  description: string;
  skill_md: string;
  version: string;
  author: string;
  category: string;
  tags: string[];
  icon: string;
  installs: number;
  verified: boolean;
  source_type: string;
  files: string[];
}

export interface ImportSkillParams {
  file_upload_id: string;
}

export interface ImportSkillFromGitHubParams {
    github_url: string;
}

export interface ImportSkillResponse {
  status: string;
  message: string;
}

/**
 * 将后端 SkillInstalledItem 映射为兼容 SkillCard 组件的 SkillMarketplaceItem。
 * 用 name 作为 skill_id（卸载接口使用 name 作为标识符）。
 */
export function installedToCardItem(item: SkillInstalledItem): SkillMarketplaceItem {
  return {
    source_type: item.source,
    skill_id: item.name,
    name: item.name,
    description: item.description,
    version: "",
    author: item.source,
    category: item.category,
    tags: item.trust ? [item.trust] : [],
    icon: "",
    installs: 0,
  };
}

function cleanParams(
  params: SearchSkillMarketplaceParams,
): Record<string, string | number | boolean | string[]> {
  const result: Record<string, string | number | boolean | string[]> = {};
  if (params.keyword) result.keyword = params.keyword;
  if (params.category) result.category = params.category;
  if (params.source_types?.length) result.source_types = params.source_types;
  if (params.limit !== undefined) result.limit = params.limit;
  return result;
}

export const skillMarketplaceApi = {
  search: (params: SearchSkillMarketplaceParams) =>
    apiClient.get<BackendDataResponse<SearchSkillMarketplaceResponse>>(
      "/skill-marketplace/search",
      { timeout: 180_000, params: cleanParams(params) },
    ),

  install: (params: InstallSkillParams) =>
    apiClient.post<BackendDataResponse<InstallSkillResponse>>(
      "/skill-marketplace/install",
      params,
    ),

  installed: () =>
    apiClient.post<BackendDataResponse<InstalledSkillsResponse>>(
      "/skill-marketplace/installed",
      {},
    ),

  uninstall: (params: UninstallSkillParams) =>
    apiClient.post<BackendDataResponse<UninstallSkillResponse>>(
      "/skill-marketplace/uninstall",
      params,
    ),

  getDetail: (params: SkillDetailParams) =>
    apiClient.post<BackendDataResponse<SkillDetailData>>(
      "/skill-marketplace/skill-detail",
      params,
      { timeout: 120_000 },
    ),

  import: (params: ImportSkillParams) =>
    apiClient.post<BackendDataResponse<ImportSkillResponse>>(
      "/skill-marketplace/import",
      params,
    ),

    importFromGitHub: (params: ImportSkillFromGitHubParams) =>
        apiClient.post<BackendDataResponse<ImportSkillResponse>>(
            "/skill-marketplace/import/github",
            params,
        ),
};
