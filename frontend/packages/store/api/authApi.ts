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
};

export type AuthTokenResponse = {
	login_status: string;
	jwt_token: string;
	refresh_token: string;
	expired_at: number;
	uin: number;
	user_info: AuthUserInfo;
	org: AuthOrgInfo;
};

const AUTH_ENDPOINTS = {
	loginByEmail: "/LoginByEmail",
	registerByEmail: "/RegisterByEmail",
	sendPhoneLoginCode: "/SendPhoneLoginCode",
	loginByPhoneCode: "/LoginByPhoneCode",
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
};
