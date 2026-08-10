import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type OrgInfo = {
	public_id: string;
	type: string;
	code: string;
	name: string;
	status: string;
	description?: string;
	logo?: string;
	address?: string;
	website?: string;
	created_at: string;
	updated_at: string;
};

export type Department = {
	id: number;
	name: string;
	parent_id: number;
	sort: number;
	org_id: number;
	created_at: string;
	updated_at: string;
};

export type ListDepartmentsResponse = {
	total: number;
	offset: number;
	limit: number;
	items: Department[];
};

export type User = {
	id: number;
	public_id: string;
	uin?: number;
	name?: string;
	phone?: string;
	email?: string;
	avatar_url?: string;
	created_at?: string;
	updated_at?: string;
};

export type ListUsersResponse = {
	total: number;
	offset: number;
	limit: number;
	items: User[];
};

const ENDPOINTS = {
	getOrg: "/GetOrg",
	updateOrg: "/UpdateOrg",
	listDepartments: "/ListDepartment",
	createDepartment: "/CreateDepartment",
	updateDepartment: "/UpdateDepartment",
	deleteDepartment: "/DeleteDepartment",
	listUsers: "/ListUser",
	createUser: "/CreateUser",
	updateUser: "/UpdateUser",
	deleteUser: "/DeleteUser",
};

const getOrgInflight = new Map<
	string,
	ReturnType<typeof apiClient.post<BackendDataResponse<OrgInfo>>>
>();

export const orgAdminApi = {
	getOrg: (params: { public_id: string }) => {
		const inflight = getOrgInflight.get(params.public_id);
		if (inflight) return inflight;

		const promise = apiClient
			.post<BackendDataResponse<OrgInfo>>(ENDPOINTS.getOrg, params)
			.finally(() => {
				if (getOrgInflight.get(params.public_id) === promise) {
					getOrgInflight.delete(params.public_id);
				}
			});
		getOrgInflight.set(params.public_id, promise);
		return promise;
	},

	updateOrg: (params: {
		public_id: string;
		name?: string;
		description?: string;
		logo?: string;
		address?: string;
		website?: string;
	}) => apiClient.post<BackendDataResponse<OrgInfo>>(ENDPOINTS.updateOrg, params),

	listDepartments: (params: { org_id: number; list_all?: boolean; keyword?: string }) =>
		apiClient.post<BackendDataResponse<ListDepartmentsResponse>>(ENDPOINTS.listDepartments, {
			org_id: params.org_id,
			list_all: params.list_all ?? true,
			keyword: params.keyword,
		}),

	createDepartment: (params: { org_id: number; name: string; parent_id?: number }) =>
		apiClient.post<BackendDataResponse<Department>>(ENDPOINTS.createDepartment, params),

	updateDepartment: (params: { id: number; name?: string; parent_id?: number; sort?: number }) =>
		apiClient.post<BackendDataResponse<Department>>(ENDPOINTS.updateDepartment, {
			id: params.id,
			name: params.name,
			parent_id: params.parent_id,
			sort: params.sort,
		}),

	deleteDepartment: (params: { id: number }) =>
		apiClient.post<BackendDataResponse<null>>(ENDPOINTS.deleteDepartment, params),

	listUsers: (params: { department_id?: number; list_all?: boolean; keyword?: string }) =>
		apiClient.post<BackendDataResponse<ListUsersResponse>>(ENDPOINTS.listUsers, {
			department_id: params.department_id,
			list_all: params.list_all ?? true,
			keyword: params.keyword,
		}),

	createUser: (params: {
		name: string;
		phone?: string;
		email?: string;
		department_ids: number[];
	}) =>
		apiClient.post<BackendDataResponse<User>>(ENDPOINTS.createUser, {
			name: params.name,
			phone: params.phone,
			email: params.email,
			department_ids: params.department_ids,
		}),

	updateUser: (params: { public_id: string; name?: string }) =>
		apiClient.post<BackendDataResponse<User>>(ENDPOINTS.updateUser, {
			public_id: params.public_id,
			name: params.name,
		}),

	deleteUser: (params: { public_id: string }) =>
		apiClient.post<BackendDataResponse<null>>(ENDPOINTS.deleteUser, params),
};
