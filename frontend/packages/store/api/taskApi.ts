import { apiClient } from "./client";
import type { BackendDataResponse, BackendPaginatedResponse, BackendTask } from "./types";

export type CreateTaskParams = {
	project_id: number;
	title: string;
	description?: string;
	assignee_id?: number;
	task_type?: string;
	deadline?: string;
	metadata?: Record<string, unknown>;
};

export type ListTasksParams = {
	project_id?: number;
	assignee_id?: number;
	keyword?: string;
	status?: string;
	task_type?: string;
	list_all?: boolean;
	offset?: number;
	limit?: number;
};

export type GetTaskParams = {
	public_id?: string;
};

export type UpdateTaskParams = {
	public_id: string;
	project_id?: number;
	title?: string;
	description?: string;
	status?: string;
	assignee_id?: number;
	task_type?: string;
	deadline?: string;
	metadata?: Record<string, unknown>;
};

export type DeleteTaskParams = {
	public_id: string;
};

const TASK_ENDPOINTS = {
	create: "/CreateTask",
	list: "/ListTasks",
	get: "/GetTask",
	update: "/UpdateTask",
	delete: "/DeleteTask",
};

export const taskApi = {
	list: (params: ListTasksParams = {}) =>
		apiClient.post<BackendPaginatedResponse<BackendTask>>(TASK_ENDPOINTS.list, params),

	create: (params: CreateTaskParams) =>
		apiClient.post<BackendDataResponse<BackendTask>>(TASK_ENDPOINTS.create, params),

	get: (params: GetTaskParams) =>
		apiClient.post<BackendDataResponse<BackendTask>>(TASK_ENDPOINTS.get, params),

	update: (params: UpdateTaskParams) =>
		apiClient.post<BackendDataResponse<BackendTask>>(TASK_ENDPOINTS.update, params),

	delete: (params: DeleteTaskParams) =>
		apiClient.post<BackendDataResponse<null>>(TASK_ENDPOINTS.delete, params),
};
