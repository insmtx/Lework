import { apiClient } from "./client";
import type { BackendDataResponse, BackendPaginatedResponse } from "./types";

export type CreateModelParams = {
	name?: string;
	description?: string;
	provider?: string;
	model: string;
	base_url: string;
	api_key: string;
	max_tokens?: number;
	temperature?: number;
	status?: string;
	purpose?: string;
	is_default?: boolean;
	config?: Record<string, unknown>;
};

export type UpdateModelParams = {
	id: number;
	name?: string;
	description?: string;
	provider?: string;
	model?: string;
	base_url?: string;
	api_key?: string;
	max_tokens?: number;
	temperature?: number;
	purpose?: string;
	is_default?: boolean;
	config?: Record<string, unknown>;
};

export type ListModelsParams = {
	provider?: string;
	status?: string;
	purpose?: string;
	keyword?: string;
	list_all?: boolean;
	offset?: number;
	limit?: number;
};

export type GetModelParams = {
	id?: number;
	code?: string;
};

export type BackendModel = {
	id: number;
	org_id: number;
	code: string;
	name: string;
	description: string;
	provider: string;
	model: string;
	base_url: string;
	base_url_has_v1: boolean;
	api_key: string;
	max_tokens: number;
	temperature: number;
	timeout_sec: number;
	status: string;
	purpose?: string;
	is_default: boolean;
	is_system: boolean;
	config?: Record<string, unknown>;
	created_at: string;
	updated_at: string;
};

export type TestModelParams = {
	id?: number;
	code?: string;
	provider?: string;
	model?: string;
	base_url?: string;
	api_key?: string;
};

export type TestModelResult = {
	success: boolean;
	status_code: number;
	message: string;
	endpoint: string;
	latency_ms: number;
	base_url_has_v1: boolean;
};

const MODEL_ENDPOINTS = {
	create: "/CreateLLMModel",
	get: "/GetLLMModel",
	getDefault: "/GetDefaultLLMModel",
	update: "/UpdateLLMModel",
	setStatus: "/SetLLMModelStatus",
	delete: "/DeleteLLMModel",
	list: "/ListLLMModels",
	test: "/TestLLMModel",
};

export const modelApi = {
	create: (params: CreateModelParams) =>
		apiClient.post<BackendDataResponse<BackendModel>>(MODEL_ENDPOINTS.create, params),
	get: (params: GetModelParams) =>
		apiClient.post<BackendDataResponse<BackendModel>>(MODEL_ENDPOINTS.get, params),
	getDefault: () =>
		apiClient.post<BackendDataResponse<BackendModel>>(MODEL_ENDPOINTS.getDefault, {}),
	update: (params: UpdateModelParams) =>
		apiClient.post<BackendDataResponse<BackendModel>>(MODEL_ENDPOINTS.update, params),
	setStatus: (id: number, status: string) =>
		apiClient.post<BackendDataResponse<BackendModel>>(MODEL_ENDPOINTS.setStatus, { id, status }),
	delete: (id: number) => apiClient.post<BackendDataResponse<null>>(MODEL_ENDPOINTS.delete, { id }),
	list: (params: ListModelsParams = {}) =>
		apiClient.post<BackendPaginatedResponse<BackendModel>>(MODEL_ENDPOINTS.list, params),
	test: (params: TestModelParams) =>
		apiClient.post<BackendDataResponse<TestModelResult>>(MODEL_ENDPOINTS.test, params),
};
