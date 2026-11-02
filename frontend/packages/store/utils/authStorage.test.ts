// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { API_BASE_URL, SERVER_CONFIG_STORAGE_KEY } from "../api/config";
import {
	AUTH_STORAGE_KEY,
	clearStoredAuthUser,
	getValidJwtToken,
	readStoredAuthUser,
	saveServerConnection,
	writeStoredAuthUser,
} from "./authStorage";

const user = {
	name: "Old",
	email: "old@example.com",
	jwtToken: "old-jwt",
	refreshToken: "old-refresh",
	expiredAt: 1,
	uin: 1,
	userId: 1,
};
const payload = {
	data: {
		jwt_token: "refreshed-old-jwt",
		refresh_token: "refreshed-old-refresh",
		expired_at: 9999999999,
		uin: 1,
		user_id: 1,
		org: { id: 1, uin: 1, public_id: "org", code: "org", name: "Org" },
		organizations: [],
		user_info: { id: 1, public_id: "u", name: "Old", email: "old@example.com" },
	},
};
beforeEach(() => {
	localStorage.clear();
	localStorage.setItem(SERVER_CONFIG_STORAGE_KEY, API_BASE_URL);
	localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(user));
});
afterEach(() => vi.unstubAllGlobals());
it("a refresh completed after switching endpoints cannot restore or return old credentials", async () => {
	let finish!: (r: Response) => void;
	vi.stubGlobal(
		"fetch",
		vi.fn(
			() =>
				new Promise((resolve) => {
					finish = resolve;
				}),
		),
	);
	const token = getValidJwtToken();
	saveServerConnection("http://new.local:38081/v1");
	finish(new Response(JSON.stringify(payload), { status: 200 }));
	expect(await token).toBeNull();
	expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull();
	writeStoredAuthUser(user);
	expect(localStorage.getItem(AUTH_STORAGE_KEY)).toBeNull();
});
it("a stale failed refresh cannot clear a new login in the same endpoint", async () => {
	let finish!: (r: Response) => void;
	vi.stubGlobal(
		"fetch",
		vi.fn(
			() =>
				new Promise((resolve) => {
					finish = resolve;
				}),
		),
	);
	const token = getValidJwtToken();
	clearStoredAuthUser();
	writeStoredAuthUser({
		...user,
		jwtToken: "new-session",
		refreshToken: "new-refresh",
		expiredAt: 9999999999,
	});
	finish(new Response("", { status: 401 }));
	expect(await token).toBeNull();
	expect(readStoredAuthUser()?.jwtToken).toBe("new-session");
});
it("a stale successful refresh cannot overwrite a new login", async () => {
	let finish!: (r: Response) => void;
	vi.stubGlobal(
		"fetch",
		vi.fn(
			() =>
				new Promise((resolve) => {
					finish = resolve;
				}),
		),
	);
	const token = getValidJwtToken();
	clearStoredAuthUser();
	writeStoredAuthUser({ ...user, jwtToken: "new-session", expiredAt: 9999999999 });
	finish(new Response(JSON.stringify(payload), { status: 200 }));
	expect(await token).toBeNull();
	expect(readStoredAuthUser()?.jwtToken).toBe("new-session");
});
it("still persists a refresh for the unchanged endpoint and session", async () => {
	vi.stubGlobal(
		"fetch",
		vi.fn().mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 })),
	);
	expect(await getValidJwtToken()).toBe("refreshed-old-jwt");
	expect(readStoredAuthUser()?.jwtToken).toBe("refreshed-old-jwt");
});
it("an old endpoint runtime cannot read or clear a new endpoint session", () => {
	localStorage.setItem(SERVER_CONFIG_STORAGE_KEY, "http://new.local:38081/v1");
	localStorage.setItem(
		AUTH_STORAGE_KEY,
		JSON.stringify({ ...user, jwtToken: "new-server-session" }),
	);
	expect(readStoredAuthUser()).toBeNull();
	clearStoredAuthUser();
	expect(JSON.parse(localStorage.getItem(AUTH_STORAGE_KEY) ?? "null").jwtToken).toBe(
		"new-server-session",
	);
});
