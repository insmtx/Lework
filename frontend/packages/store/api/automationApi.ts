import { apiClient } from "./client";
import type {
	BackendAutomation,
	BackendAutomationExecution,
	BackendAutomationScheduleInput,
	BackendDataResponse,
	BackendPaginatedResponse,
} from "./types";

export type CreateAutomationParams = {
	name: string;
	instruction?: string;
	enabled?: boolean;
	schedule_mode: string;
	schedule: BackendAutomationScheduleInput;
	timezone?: string;
	// 关联既有项目（可选）；省略/空串 => 默认新项目（首次执行懒创建）
	project_public_id?: string;
};

export type UpdateAutomationParams = {
	public_id: string;
	name?: string;
	instruction?: string;
	enabled?: boolean;
	schedule_mode?: string;
	schedule?: BackendAutomationScheduleInput;
	timezone?: string;
	// 关联项目三态：省略=保持原关联；""=切回默认新项目；非空=关联指定项目
	project_public_id?: string;
};

export type GetAutomationParams = {
	public_id?: string;
};

export type DeleteAutomationParams = {
	public_id: string;
};

export type ListAutomationsParams = {
	keyword?: string;
	enabled?: boolean;
	schedule_mode?: string;
	offset?: number;
	limit?: number;
};

export type RunAutomationNowParams = {
	public_id: string;
};

export type ListAutomationExecutionsParams = {
	public_id: string;
	status?: string;
	offset?: number;
	limit?: number;
};

export type GetAutomationExecutionParams = {
	public_id: string;
};

const AUTOMATION_ENDPOINTS = {
	create: "/CreateAutomation",
	list: "/ListAutomations",
	get: "/GetAutomation",
	update: "/UpdateAutomation",
	delete: "/DeleteAutomation",
	runNow: "/RunAutomationNow",
	listExecutions: "/ListAutomationExecutions",
	getExecution: "/GetAutomationExecution",
};

export const automationApi = {
	list: (params: ListAutomationsParams = {}) =>
		apiClient.post<BackendPaginatedResponse<BackendAutomation>>(AUTOMATION_ENDPOINTS.list, params),

	create: (params: CreateAutomationParams) =>
		apiClient.post<BackendDataResponse<BackendAutomation>>(AUTOMATION_ENDPOINTS.create, params),

	get: (params: GetAutomationParams) =>
		apiClient.post<BackendDataResponse<BackendAutomation>>(AUTOMATION_ENDPOINTS.get, params),

	update: (params: UpdateAutomationParams) =>
		apiClient.post<BackendDataResponse<BackendAutomation>>(AUTOMATION_ENDPOINTS.update, params),

	delete: (params: DeleteAutomationParams) =>
		apiClient.post<BackendDataResponse<null>>(AUTOMATION_ENDPOINTS.delete, params),

	runNow: (params: RunAutomationNowParams) =>
		apiClient.post<BackendDataResponse<BackendAutomationExecution>>(
			AUTOMATION_ENDPOINTS.runNow,
			params,
		),

	listExecutions: (params: ListAutomationExecutionsParams) =>
		apiClient.post<BackendPaginatedResponse<BackendAutomationExecution>>(
			AUTOMATION_ENDPOINTS.listExecutions,
			params,
		),

	getExecution: (params: GetAutomationExecutionParams) =>
		apiClient.post<BackendDataResponse<BackendAutomationExecution>>(
			AUTOMATION_ENDPOINTS.getExecution,
			params,
		),
};
