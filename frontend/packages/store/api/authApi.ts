import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type RegisterByEmailParams = {
	email: string;
	password: string;
	confirm_password: string;
	name?: string;
};

export type LoginByEmailParams = {
	email: string;
	password: string;
	org_id?: number;
};

export type SendPhoneLoginCodeParams = {
	phone: string;
};

export type SendPhoneLoginCodeResponse = {
	phone: string;
	expires_in: number;
	resend_after?: number;
};

export type LoginByPhoneCodeParams = {
	phone: string;
	code: string;
	org_id?: number;
};

export type SwitchOrganizationParams = {
	org_id: number;
};

export type CreateOrganizationParams = {
	name: string;
};

export type AuthUserInfo = {
	id: number;
	public_id: string;
	name: string;
	email: string;
	phone?: string;
	github_login?: string;
	avatar_url?: string;
};

export type AuthOrgInfo = {
	id: number;
	public_id: string;
	code: string;
	name: string;
	logo?: string;
	is_default?: boolean;
};

export type AuthSessionResponse = {
	user_info: AuthUserInfo;
	org: AuthOrgInfo;
	organizations?: AuthOrgInfo[];
};

export type AuthTokenResponse = {
	login_status: string;
	jwt_token: string;
	refresh_token: string;
	expired_at: number;
	uin: number;
	user_info: AuthUserInfo;
	org: AuthOrgInfo;
	organizations?: AuthOrgInfo[];
};

const AUTH_ENDPOINTS = {
	loginByEmail: "/LoginByEmail",
	registerByEmail: "/RegisterByEmail",
	sendPhoneLoginCode: "/SendPhoneLoginCode",
	loginByPhoneCode: "/LoginByPhoneCode",
	refreshToken: "/RefreshToken",
	switchOrganization: "/SwitchOrganization",
	createOrganization: "/CreateOrganization",
	authSession: "/AuthSession",
};

export const authApi = {
	loginByEmail: (params: LoginByEmailParams) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(AUTH_ENDPOINTS.loginByEmail, params),

	registerByEmail: (params: RegisterByEmailParams) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(AUTH_ENDPOINTS.registerByEmail, params),

	sendPhoneLoginCode: (params: SendPhoneLoginCodeParams) =>
		apiClient.post<BackendDataResponse<SendPhoneLoginCodeResponse>>(
			AUTH_ENDPOINTS.sendPhoneLoginCode,
			params,
		),

	loginByPhoneCode: (params: LoginByPhoneCodeParams) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(AUTH_ENDPOINTS.loginByPhoneCode, params),

	refreshToken: (refreshToken: string) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(AUTH_ENDPOINTS.refreshToken, {
			refresh_token: refreshToken,
		}),

	switchOrganization: (params: SwitchOrganizationParams | number) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(
			AUTH_ENDPOINTS.switchOrganization,
			typeof params === "number" ? { org_id: params } : params,
		),

	createOrganization: (params: CreateOrganizationParams) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(
			AUTH_ENDPOINTS.createOrganization,
			params,
		),

	authSession: () =>
		apiClient.get<BackendDataResponse<AuthSessionResponse>>(AUTH_ENDPOINTS.authSession),
};
