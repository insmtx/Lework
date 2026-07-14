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

export type OrgMemberDepartment = {
	id: number;
	department_id: number;
	name: string;
	is_primary: boolean;
};

export type OrgMember = {
	id: number;
	uin: number;
	user_id: string;
	org_id: string;
	is_default: boolean;
	user_name?: string;
	user_login?: string;
	user_phone?: string;
	avatar_url?: string;
	org_name?: string;
	departments?: OrgMemberDepartment[];
	created_at: string;
	updated_at: string;
};

export type ListOrgMembersResponse = {
	total: number;
	offset: number;
	limit: number;
	items: OrgMember[];
};

const ENDPOINTS = {
	getOrg: "/GetOrg",
	updateOrg: "/UpdateOrg",
	listDepartments: "/ListDepartments",
	createDepartment: "/CreateDepartment",
	updateDepartment: "/UpdateDepartment",
	deleteDepartment: "/DeleteDepartment",
	listOrgMembers: "/ListOrgMembers",
	createOrgMember: "/CreateOrgMember",
	updateOrgMember: "/UpdateOrgMember",
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

	listOrgMembers: (params: { org_id: number; department_id?: number; list_all?: boolean }) =>
		apiClient.post<BackendDataResponse<ListOrgMembersResponse>>(ENDPOINTS.listOrgMembers, {
			org_id: params.org_id,
			department_id: params.department_id,
			list_all: params.list_all ?? true,
		}),

	createOrgMember: (params: { name: string; phone: string; department_ids: number[] }) =>
		apiClient.post<BackendDataResponse<OrgMember>>(ENDPOINTS.createOrgMember, {
			name: params.name,
			phone: params.phone,
			department_ids: params.department_ids,
		}),

	updateOrgMember: (params: { id: number; name?: string; department_ids?: number[] }) =>
		apiClient.post<BackendDataResponse<OrgMember>>(ENDPOINTS.updateOrgMember, {
			id: params.id,
			name: params.name,
			department_ids: params.department_ids,
		}),
};
