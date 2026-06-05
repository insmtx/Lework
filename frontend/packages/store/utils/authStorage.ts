export type StoredAuthUser = {
	name: string;
	email: string;
	jwtToken?: string;
	refreshToken?: string;
	expiredAt?: number;
	uin?: number;
};

export const AUTH_STORAGE_KEY = "leros-auth-user";

export function readStoredAuthUser(): StoredAuthUser | null {
	if (typeof window === "undefined") return null;

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
	if (typeof window === "undefined") return;

	try {
		window.localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(user));
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
