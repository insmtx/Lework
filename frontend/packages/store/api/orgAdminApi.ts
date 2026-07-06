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

const ENDPOINTS = {
	getOrg: "/GetOrg",
	updateOrg: "/UpdateOrg",
	listDepartments: "/ListAccountDepartments",
	createDepartment: "/CreateAccountDepartment",
	updateDepartment: "/UpdateAccountDepartment",
	deleteDepartment: "/DeleteAccountDepartment",
};

export const orgAdminApi = {
	getOrg: (params: { public_id: string }) =>
		apiClient.post<BackendDataResponse<OrgInfo>>(ENDPOINTS.getOrg, params),

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
			limit: 200,
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
};
