import { apiClient } from "./client";
import type {
	BackendDataResponse,
	BackendDigitalAssistant,
	BackendPaginatedResponse,
} from "./types";

export type CreateDAParams = {
	code: string;
	name: string;
	description?: string;
	avatar?: string;
	system_prompt?: string;
};

export type UpdateDAParams = {
	id: number;
	name?: string;
	description?: string;
	avatar?: string;
	system_prompt?: string;
};

export type UpdateDAStatusParams = {
	id: number;
	status: string;
};

export type ListDAParams = {
	keyword?: string;
	status?: string;
	list_all?: boolean;
	offset?: number;
	limit?: number;
};

export type GetDAParams = {
	id?: number;
	code?: string;
};

const DA_ENDPOINTS = {
	create: "/CreateDigitalAssistant",
	list: "/ListDigitalAssistant",
	get: "/GetDigitalAssistant",
	update: "/UpdateDigitalAssistant",
	updateStatus: "/UpdateDigitalAssistantStatus",
	delete: "/DeleteDigitalAssistant",
};

export const digitalAssistantApi = {
	create: (params: CreateDAParams) =>
		apiClient.post<BackendDataResponse<BackendDigitalAssistant>>(DA_ENDPOINTS.create, params),

	list: (params: ListDAParams) =>
		apiClient.post<BackendPaginatedResponse<BackendDigitalAssistant>>(DA_ENDPOINTS.list, params),

	get: (params: GetDAParams) =>
		apiClient.post<BackendDataResponse<BackendDigitalAssistant>>(DA_ENDPOINTS.get, params),

	update: (params: UpdateDAParams) =>
		apiClient.post<BackendDataResponse<BackendDigitalAssistant>>(DA_ENDPOINTS.update, params),

	updateStatus: (params: UpdateDAStatusParams) =>
		apiClient.post<BackendDataResponse<null>>(DA_ENDPOINTS.updateStatus, params),

	delete: (id: number) => apiClient.post<BackendDataResponse<null>>(DA_ENDPOINTS.delete, { id }),
};
