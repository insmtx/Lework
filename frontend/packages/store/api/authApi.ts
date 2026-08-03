import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type RegisterByEmailParams = {
	email: string;
	password: string;
	confirm_password: string;
	name?: string;
};

export type LoginByPasswordParams = {
	account: string;
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
	org_id?: number;
};

export type CreateOrganizationParams = {
	name: string;
	user_id: number;
	user_display_name: string;
};

export type CreateOrganizationForPendingLoginParams = CreateOrganizationParams & {
	refresh_token: string;
};

export type CreateOrganizationResponse = {
	uin: number;
	org: AuthOrgInfo;
};

export type ChooseUinParams = {
	refresh_token: string;
	uin: number;
	user_id: number;
	login_way?: number;
};

export type RefreshTokenParams = {
	refresh_token: string;
	iam_uin_id: number;
	iam_user_id: number;
	login_way?: number;
};

export type AuthUserInfo = {
	id: number;
	public_id: string;
	name: string;
	email: string;
	phone?: string;
	github_login?: string;
	avatar_url?: string;
	uin_name?: string;
};

export type AuthOrgInfo = {
	id: number;
	uin: number;
	public_id: string;
	code: string;
	name: string;
	logo?: string;
	is_default?: boolean;
	created_by_uin?: number;
	created_by_user_id?: number;
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
	user_id: number;
	login_way?: number;
	user_info: AuthUserInfo;
	org: AuthOrgInfo;
	organizations?: AuthOrgInfo[];
};

// 中文注释：统一账号密码登录，account 支持邮箱或手机号，统一返回 refresh_token 不签发 JWT，需后续调用 ChooseUin 选择组织。
export type LoginByPasswordResponse = {
	login_status: string;
	user_id: number;
	refresh_token: string;
	user_info: AuthUserInfo;
	organizations?: AuthOrgInfo[];
	login_way?: number;
};
export type SwitchOrganizationResponse = Pick<AuthTokenResponse, "login_status" | "jwt_token">;

// 中文注释：手机号已验证但尚未选定组织时，仅持有刷新凭证，不能访问组织业务接口。
export type PendingOrganizationLoginResponse = {
	login_status: string;
	refresh_token: string;
	user_id: number;
	user_info: AuthUserInfo;
	login_way?: number;
	organizations?: AuthOrgInfo[];
};

const AUTH_ENDPOINTS = {
	loginByPassword: "/LoginByPassword",
	registerByEmail: "/RegisterByEmail",
	sendPhoneLoginCode: "/SendPhoneLoginCode",
	loginByPhoneCode: "/LoginByPhoneCode",
	refreshToken: "/RefreshToken",
	switchOrganization: "/SwitchOrganization",
	createOrganization: "/CreateOrganization",
	chooseUin: "/ChooseUin",
	authSession: "/AuthSession",
};

export const authApi = {
	loginByPassword: (params: LoginByPasswordParams) =>
		apiClient.post<BackendDataResponse<LoginByPasswordResponse>>(
			AUTH_ENDPOINTS.loginByPassword,
			params,
		),

	registerByEmail: (params: RegisterByEmailParams) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(AUTH_ENDPOINTS.registerByEmail, params),

	sendPhoneLoginCode: (params: SendPhoneLoginCodeParams) =>
		apiClient.post<BackendDataResponse<SendPhoneLoginCodeResponse>>(
			AUTH_ENDPOINTS.sendPhoneLoginCode,
			params,
		),

	loginByPhoneCode: (params: LoginByPhoneCodeParams) =>
		apiClient.post<BackendDataResponse<PendingOrganizationLoginResponse>>(
			AUTH_ENDPOINTS.loginByPhoneCode,
			params,
		),

	refreshToken: (params: RefreshTokenParams) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(AUTH_ENDPOINTS.refreshToken, params),

	switchOrganization: (uin: number) =>
		apiClient.post<BackendDataResponse<SwitchOrganizationResponse>>(
			AUTH_ENDPOINTS.switchOrganization,
			{
				uin,
			},
		),

	createOrganization: (params: CreateOrganizationParams) =>
		apiClient.post<BackendDataResponse<CreateOrganizationResponse>>(
			AUTH_ENDPOINTS.createOrganization,
			params,
		),

	createOrganizationForPendingLogin: (params: CreateOrganizationForPendingLoginParams) =>
		apiClient.post<BackendDataResponse<CreateOrganizationResponse>>(
			AUTH_ENDPOINTS.createOrganization,
			params,
		),

	chooseUin: (params: ChooseUinParams) =>
		apiClient.post<BackendDataResponse<AuthTokenResponse>>(AUTH_ENDPOINTS.chooseUin, params),

	authSession: () =>
		apiClient.get<BackendDataResponse<AuthSessionResponse>>(AUTH_ENDPOINTS.authSession),
};
