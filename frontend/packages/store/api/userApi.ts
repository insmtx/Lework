import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type UserInfo = {
	public_id: string;
	github_id?: number;
	github_login?: string;
	name: string;
	email?: string;
	phone?: string;
	avatar_url?: string;
	bio?: string;
	company?: string;
	location?: string;
	created_at: string;
	updated_at: string;
};

export type UpdateUserParams = {
	public_id: string;
	name?: string;
	avatar_url?: string;
	email?: string;
	bio?: string;
	company?: string;
	location?: string;
};

export const userApi = {
	update: (params: UpdateUserParams) =>
		apiClient.post<BackendDataResponse<UserInfo>>("/UpdateUser", params),
};
