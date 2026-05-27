import { apiClient } from "./client";
import type { BackendDataResponse, BackendPaginatedResponse, BackendProject, BackendProjectDetail } from "./types";

export type CreateProjectParams = {
	name: string;
	description?: string;
	objective?: string;
	metadata?: Record<string, unknown>;
};

export type ListProjectsParams = {
	keyword?: string;
	status?: string;
	list_all?: boolean;
	offset?: number;
	limit?: number;
};

export type GetProjectParams = {
	public_id?: string;
};

export type UpdateProjectParams = {
	public_id: string;
	name?: string;
	description?: string;
	objective?: string;
	status?: string;
	owner_id?: number;
	metadata?: Record<string, unknown>;
};

export type DeleteProjectParams = {
	public_id: string;
};

const PROJECT_ENDPOINTS = {
	create: "/CreateProject",
	list: "/ListProjects",
	get: "/GetProject",
	detail: "/DetailProject",
	update: "/UpdateProject",
	delete: "/DeleteProject",
};

export const projectApi = {
	list: (params: ListProjectsParams = {}) =>
		apiClient.post<BackendPaginatedResponse<BackendProject>>(PROJECT_ENDPOINTS.list, params),

	create: (params: CreateProjectParams) =>
		apiClient.post<BackendDataResponse<BackendProject>>(PROJECT_ENDPOINTS.create, params),

	get: (params: GetProjectParams) =>
		apiClient.post<BackendDataResponse<BackendProject>>(PROJECT_ENDPOINTS.get, params),

	update: (params: UpdateProjectParams) =>
		apiClient.post<BackendDataResponse<BackendProject>>(PROJECT_ENDPOINTS.update, params),

	detail: (params: GetProjectParams) =>
		apiClient.post<BackendDataResponse<BackendProjectDetail>>(PROJECT_ENDPOINTS.detail, params),

	delete: (params: DeleteProjectParams) =>
		apiClient.post<BackendDataResponse<null>>(PROJECT_ENDPOINTS.delete, params),
};
