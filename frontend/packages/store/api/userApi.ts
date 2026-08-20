import { apiClient } from "./client";
import type { BackendDataResponse, BackendPaginatedResponse } from "./types";

export type UserInfo = {
	id?: number;
	public_id: string;
	github_id?: number;
	github_login?: string;
	name: string;
	email?: string;
	phone?: string;
	avatar_url?: string;
	departments?: Array<{ department_id: number; name: string; is_primary?: boolean }>;
	bio?: string;
	company?: string;
	location?: string;
	created_at: string;
	updated_at: string;
};

export type UpdateUserParams = {
	public_id: string;
	name?: string;
	email?: string;
};

export type UpdateCurrentUserParams = {
	name?: string;
	email?: string;
	avatar_url?: string;
};

export type ListUsersParams = {
	keyword?: string;
	github_login?: string;
	offset?: number;
	limit?: number;
};

export const userApi = {
	list: (params: ListUsersParams = {}) =>
		apiClient.post<BackendPaginatedResponse<UserInfo>>("/ListUser", params),

	update: (params: UpdateUserParams) =>
		apiClient.post<BackendDataResponse<UserInfo>>("/UpdateUser", params),

	updateCurrent: (params: UpdateCurrentUserParams) =>
		apiClient.post<BackendDataResponse<UserInfo>>("/UpdateCurrentUser", params),
};
