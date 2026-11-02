import {
	API_BASE_URL,
	normalizeAPIBaseURL,
	readServerBaseURL,
	saveServerBaseURL,
} from "../api/config";

export type StoredAuthOrg = {
	id: number;
	uin: number;
	publicId?: string;
	code: string;
	name: string;
	logo?: string;
	isDefault?: boolean;
	isAdmin?: boolean;
	createdByUin?: number;
	createdByUserId?: number;
};

export type StoredAuthUser = {
	publicId?: string;
	name: string;
	uinName?: string;
	email: string;
	phone?: string;
	avatarUrl?: string;
	jwtToken?: string;
	refreshToken?: string;
	expiredAt?: number;
	uin?: number;
	userId?: number;
	loginWay?: number;
	currentOrg?: StoredAuthOrg;
	organizations?: StoredAuthOrg[];
};

export const AUTH_STORAGE_KEY = "leros-auth-user";
export const AUTH_SESSION_EXPIRED_EVENT = "leros-auth-session-expired";

const TOKEN_REFRESH_BUFFER_SECONDS = 60;
let sessionGeneration = 0;
let pendingRefresh: {
	generation: number;
	snapshot: string;
	promise: Promise<string | null>;
} | null = null;

// 旧窗口在服务切换后不得再读写新服务的会话。
function isRuntimeEndpointCurrent(): boolean {
	return (readServerBaseURL() ?? API_BASE_URL) === API_BASE_URL;
}

type RefreshTokenPayload = {
	data?: {
		jwt_token: string;
		refresh_token: string;
		expired_at: number;
		uin: number;
		user_id: number;
		login_way?: number;
		org: {
			id: number;
			uin: number;
			public_id: string;
			code: string;
			name: string;
			logo?: string;
			is_default?: boolean;
			is_admin?: boolean;
			created_by_uin?: number;
			created_by_user_id?: number;
		};
		organizations: {
			id: number;
			uin: number;
			public_id: string;
			code: string;
			name: string;
			logo?: string;
			is_default?: boolean;
			is_admin?: boolean;
			created_by_uin?: number;
			created_by_user_id?: number;
		}[];
		user_info: {
			id: number;
			public_id: string;
			name: string;
			email: string;
			phone?: string;
			avatar_url?: string;
			uin_name?: string;
		};
	};
};

export function readStoredAuthUser(): StoredAuthUser | null {
	if (typeof window === "undefined" || !isRuntimeEndpointCurrent()) return null;

	try {
		const stored = window.localStorage.getItem(AUTH_STORAGE_KEY);
		if (!stored) return null;
		return JSON.parse(stored) as StoredAuthUser;
	} catch (err) {
		console.error("read auth user error:", err);
		return null;
	}
}

export function writeStoredAuthUser(user: StoredAuthUser) {
	if (typeof window === "undefined" || !isRuntimeEndpointCurrent()) return;
	sessionGeneration++;
	pendingRefresh = null;

	try {
		window.localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(user));
	} catch (err) {
		console.error("save auth user error:", err);
	}
}

export function clearStoredAuthUser() {
	if (typeof window === "undefined" || !isRuntimeEndpointCurrent()) return;
	sessionGeneration++;
	pendingRefresh = null;

	try {
		window.localStorage.removeItem(AUTH_STORAGE_KEY);
	} catch (err) {
		console.error("clear auth user error:", err);
	}
}

/** Persist an explicitly validated endpoint without carrying credentials across services. */
export function saveServerConnection(value: string, currentURL = API_BASE_URL): string {
	const normalized = normalizeAPIBaseURL(value);
	if (typeof window === "undefined") throw new Error("当前环境不支持保存服务地址");
	// 清除失败必须中止；地址写入失败允许退出登录，但不能把旧凭据带到新服务。
	if (normalized !== normalizeAPIBaseURL(currentURL)) {
		sessionGeneration++;
		pendingRefresh = null;
		window.localStorage.removeItem(AUTH_STORAGE_KEY);
	}
	return saveServerBaseURL(normalized);
}

export function readStoredJwtToken(): string | null {
	const user = readStoredAuthUser();
	return user?.jwtToken ?? null;
}

export async function getValidJwtToken(forceRefresh = false): Promise<string | null> {
	const user = readStoredAuthUser();
	if (!user?.jwtToken) return null;

	const now = Math.floor(Date.now() / 1000);
	if (!forceRefresh && (!user.expiredAt || user.expiredAt > now + TOKEN_REFRESH_BUFFER_SECONDS)) {
		return user.jwtToken;
	}
	if (!user.refreshToken) {
		expireStoredAuthSession();
		return null;
	}

	const snapshot = window.localStorage.getItem(AUTH_STORAGE_KEY) ?? "";
	const generation = sessionGeneration;
	if (
		!pendingRefresh ||
		pendingRefresh.generation !== generation ||
		pendingRefresh.snapshot !== snapshot
	) {
		const isCurrent = () =>
			generation === sessionGeneration &&
			isRuntimeEndpointCurrent() &&
			window.localStorage.getItem(AUTH_STORAGE_KEY) === snapshot;
		const promise = refreshStoredAuthToken(user, isCurrent).finally(() => {
			if (pendingRefresh?.promise === promise) pendingRefresh = null;
		});
		pendingRefresh = { generation, snapshot, promise };
	}
	return pendingRefresh.promise;
}

export async function authenticatedFetch(
	input: RequestInfo | URL,
	init: RequestInit = {},
): Promise<Response> {
	const token = await getValidJwtToken();
	const response = await fetch(input, withAuthorization(init, token));
	if (response.status !== 401 || !token) return response;

	const refreshedToken = await getValidJwtToken(true);
	if (!refreshedToken) return response;
	return fetch(input, withAuthorization(init, refreshedToken));
}

async function refreshStoredAuthToken(
	user: StoredAuthUser,
	isCurrent: () => boolean,
): Promise<string | null> {
	try {
		if (!user.refreshToken) {
			expireStoredAuthSession();
			return null;
		}
		if (!user.uin || !user.userId) {
			expireStoredAuthSession();
			return null;
		}
		const payload = await requestRefreshToken({
			refreshToken: user.refreshToken,
			iamUinID: user.uin,
			iamUserID: user.userId,
			loginWay: user.loginWay,
			isCurrent,
		});
		if (!payload || !isCurrent()) return null;
		const token = payload.data;
		if (!token?.jwt_token || !token.refresh_token || !token.uin || !token.user_id) {
			expireStoredAuthSession();
			return null;
		}

		writeStoredAuthUser({
			...user,
			publicId: token.user_info.public_id,
			name: token.user_info.name,
			email: token.user_info.email,
			phone: token.user_info.phone,
			avatarUrl: token.user_info.avatar_url,
			jwtToken: token.jwt_token,
			refreshToken: token.refresh_token,
			expiredAt: token.expired_at,
			uin: token.uin,
			userId: token.user_id,
			loginWay: token.login_way ?? user.loginWay,
			currentOrg: toStoredAuthOrg(token.org),
			organizations: toStoredAuthOrgs(token.organizations),
		});
		return readStoredAuthUser()?.jwtToken === token.jwt_token ? token.jwt_token : null;
	} catch (err) {
		console.error("refresh auth token error:", err);
		return null;
	}
}

async function requestRefreshToken({
	refreshToken,
	iamUinID,
	iamUserID,
	loginWay: _loginWay,
	isCurrent,
}: {
	refreshToken: string;
	iamUinID: number;
	iamUserID: number;
	loginWay?: number;
	isCurrent: () => boolean;
}): Promise<RefreshTokenPayload | null> {
	const response = await fetch(`${API_BASE_URL}/RefreshToken`, {
		method: "POST",
		headers: { "Content-Type": "application/json" },
		body: JSON.stringify({
			refresh_token: refreshToken,
			iam_uin_id: iamUinID,
			iam_user_id: iamUserID,
			// 中文注释：后端当前阶段暂不要求登录方式，后续按实际契约恢复传递登录方式。
			// login_way: loginWay,
		}),
	});
	if (!response.ok) {
		if ((response.status === 400 || response.status === 401) && isCurrent()) {
			expireStoredAuthSession();
		}
		return null;
	}
	return (await response.json()) as RefreshTokenPayload;
}

function toStoredAuthOrg(org: {
	id: number;
	uin: number;
	public_id: string;
	code: string;
	name: string;
	logo?: string;
	is_default?: boolean;
	is_admin?: boolean;
	created_by_uin?: number;
	created_by_user_id?: number;
}): StoredAuthOrg {
	return {
		id: org.id,
		uin: org.uin,
		publicId: org.public_id,
		code: org.code,
		name: org.name,
		logo: org.logo,
		isDefault: org.is_default,
		isAdmin: org.is_admin,
		createdByUin: org.created_by_uin,
		createdByUserId: org.created_by_user_id,
	};
}

function toStoredAuthOrgs(orgs: Parameters<typeof toStoredAuthOrg>[0][]): StoredAuthOrg[] {
	return orgs.map((org) => toStoredAuthOrg(org));
}

function expireStoredAuthSession() {
	const hadSession = Boolean(readStoredAuthUser());
	clearStoredAuthUser();
	if (hadSession && typeof window !== "undefined") {
		window.dispatchEvent(new Event(AUTH_SESSION_EXPIRED_EVENT));
	}
}

function withAuthorization(init: RequestInit, token: string | null): RequestInit {
	if (!token) return init;
	return {
		...init,
		headers: {
			...headersToRecord(init.headers),
			Authorization: `Bearer ${token}`,
		},
	};
}

function headersToRecord(headers: HeadersInit | undefined): Record<string, string> {
	if (!headers) return {};
	if (headers instanceof Headers) return Object.fromEntries(headers.entries());
	if (Array.isArray(headers)) return Object.fromEntries(headers);
	return headers;
}
