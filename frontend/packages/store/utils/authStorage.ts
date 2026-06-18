import { API_BASE_URL } from "../api/config";

export type StoredAuthUser = {
	name: string;
	email: string;
	avatarUrl?: string;
	jwtToken?: string;
	refreshToken?: string;
	expiredAt?: number;
	uin?: number;
	apiBaseUrl?: string;
};

export const AUTH_STORAGE_KEY = "leros-auth-user";
export const AUTH_SESSION_EXPIRED_EVENT = "leros-auth-session-expired";

const TOKEN_REFRESH_BUFFER_SECONDS = 60;
let refreshPromise: Promise<string | null> | null = null;

export function readStoredAuthUser(): StoredAuthUser | null {
	if (typeof window === "undefined") return null;

	try {
		const stored = window.localStorage.getItem(AUTH_STORAGE_KEY);
		if (!stored) return null;
		const user = JSON.parse(stored) as StoredAuthUser;
		if (user.apiBaseUrl && user.apiBaseUrl !== API_BASE_URL) {
			clearStoredAuthUser();
			return null;
		}
		return user;
	} catch (err) {
		console.error("read auth user error:", err);
		return null;
	}
}

export function writeStoredAuthUser(user: StoredAuthUser) {
	if (typeof window === "undefined") return;

	try {
		window.localStorage.setItem(
			AUTH_STORAGE_KEY,
			JSON.stringify({ ...user, apiBaseUrl: API_BASE_URL }),
		);
	} catch (err) {
		console.error("save auth user error:", err);
	}
}

export function clearStoredAuthUser() {
	if (typeof window === "undefined") return;

	try {
		window.localStorage.removeItem(AUTH_STORAGE_KEY);
	} catch (err) {
		console.error("clear auth user error:", err);
	}
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

	if (!refreshPromise) {
		refreshPromise = refreshStoredAuthToken(user).finally(() => {
			refreshPromise = null;
		});
	}
	return refreshPromise;
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

async function refreshStoredAuthToken(user: StoredAuthUser): Promise<string | null> {
	try {
		const response = await fetch(`${API_BASE_URL}/RefreshToken`, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ refresh_token: user.refreshToken }),
		});
		if (!response.ok) {
			if (response.status === 400 || response.status === 401) {
				expireStoredAuthSession();
			}
			return null;
		}

		const payload = (await response.json()) as {
			data?: {
				jwt_token?: string;
				refresh_token?: string;
				expired_at?: number;
				uin?: number;
				user_info?: { name?: string; email?: string; avatar_url?: string };
			};
		};
		const token = payload.data;
		if (!token?.jwt_token || !token.refresh_token) {
			expireStoredAuthSession();
			return null;
		}

		writeStoredAuthUser({
			...user,
			name: token.user_info?.name || user.name,
			email: token.user_info?.email || user.email,
			avatarUrl: token.user_info?.avatar_url || user.avatarUrl,
			jwtToken: token.jwt_token,
			refreshToken: token.refresh_token,
			expiredAt: token.expired_at,
			uin: token.uin ?? user.uin,
		});
		return token.jwt_token;
	} catch (err) {
		console.error("refresh auth token error:", err);
		return null;
	}
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
