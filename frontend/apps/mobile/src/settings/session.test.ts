import { ApiError } from "@lework/api";
import { describe, expect, it } from "vitest";

import {
	type AuthClient,
	authErrorMessage,
	canRegister,
	clearExpiredSession,
	loadAuthMethods,
	saveServerAddress,
	signIn,
	signOut,
} from "./session";
import type { Settings, SettingsStore } from "./settings";

const baseSettings: Settings = {
	baseUrl: "http://box:8787",
	email: "a@example.com",
	sessionToken: "sess-old",
	notifications: true,
};

// fakeSettingsStore 记录 save 的每一次入参，方便断言「落盘的就是返回的」。
function fakeSettingsStore(initial: Settings | null, saved: Settings[]): SettingsStore {
	return {
		load: async () => initial,
		save: async (next) => {
			saved.push(next);
		},
		clear: async () => {},
	};
}

function fakeAuthClient(result: { token: string }): AuthClient {
	return {
		getAuthMethods: async () => ({ initialized: true, registration_mode: "open" }),
		login: async () => result,
		register: async () => result,
		logout: async () => null,
		changePassword: async () => null,
	};
}

function rejectingClient(error: unknown): AuthClient {
	const fail = async () => {
		throw error;
	};
	return {
		getAuthMethods: fail,
		login: fail,
		register: fail,
		logout: fail,
		changePassword: fail,
	};
}

describe("signIn", () => {
	it("登录成功写入会话并保留地址与通知偏好", async () => {
		const saved: Settings[] = [];
		const settings = await signIn(
			{
				store: fakeSettingsStore({ ...baseSettings, email: "", sessionToken: "" }, saved),
				client: () => fakeAuthClient({ token: "sess-1" }),
			},
			{ baseUrl: "http://box:8787", email: "a@example.com", password: "password123" },
		);
		expect(settings).toMatchObject({
			email: "a@example.com",
			sessionToken: "sess-1",
			notifications: true,
		});
		expect(saved).toHaveLength(1);
	});

	it("登录失败的提示在页面二次映射后仍不是网络错误", async () => {
		const error = new ApiError(401, "invalid_credentials", "invalid_credentials");
		let caught: unknown;
		try {
			await signIn(
				{ store: fakeSettingsStore(baseSettings, []), client: () => rejectingClient(error) },
				{ baseUrl: "http://box:8787", email: "a@example.com", password: "bad" },
			);
		} catch (thrown) {
			caught = thrown;
		}
		// 页面拿到 signIn 抛出的错误后会再调一次 authErrorMessage；它必须仍是
		// 账号密码提示，而不是把「网络不通」指给用户。
		expect(authErrorMessage(caught)).toBe("账号或密码错误");
	});

	it("地址不完整时提示怎么写而不是报网络错误", async () => {
		await expect(
			signIn(
				{ store: fakeSettingsStore(baseSettings, []), client: () => fakeAuthClient({ token: "t" }) },
				{ baseUrl: "192.168.1.10:8787", email: "a@example.com", password: "password123" },
			),
		).rejects.toThrow(/完整/);
	});

	it("401 映射为账号或密码错误", async () => {
		const error = new ApiError(401, "invalid_credentials", "invalid_credentials");
		await expect(
			signIn(
				{ store: fakeSettingsStore(baseSettings, []), client: () => rejectingClient(error) },
				{ baseUrl: "http://box:8787", email: "a@example.com", password: "bad" },
			),
		).rejects.toThrow("账号或密码错误");
	});
});

describe("signOut", () => {
	it("清会话与账号，保留地址", async () => {
		const next = await signOut(
			{ store: fakeSettingsStore(baseSettings, []), client: () => fakeAuthClient({ token: "" }) },
			baseSettings,
		);
		expect(next).toMatchObject({ baseUrl: "http://box:8787", email: "", sessionToken: "" });
	});
});

describe("clearExpiredSession", () => {
	it("401 后只清登录态", async () => {
		const next = await clearExpiredSession(fakeSettingsStore(baseSettings, []), baseSettings);
		expect(next.sessionToken).toBe("");
		expect(next.baseUrl).toBe("http://box:8787");
	});
});

describe("authErrorMessage", () => {
	it.each([
		[new ApiError(401, "invalid_credentials", ""), "账号或密码错误"],
		[new ApiError(403, "registration_closed", ""), "这台服务器未开放注册"],
		[new ApiError(429, "too_many_attempts", ""), "尝试次数过多，请稍后再试"],
		[new TypeError("Network request failed"), "网络不通，请检查服务器地址"],
	])("映射 %s", (error, want) => {
		expect(authErrorMessage(error)).toBe(want);
	});
});

describe("loadAuthMethods", () => {
	it("未初始化服务器的提示来自 auth/methods", async () => {
		const methods = await loadAuthMethods(
			{ client: () => fakeAuthClient({ token: "" }) },
			"http://box:8787",
		);
		expect(methods).toEqual({ initialized: true, registration_mode: "open" });
	});
});

describe("saveServerAddress", () => {
	it("保存服务器地址会清掉旧会话（不同控制面的会话不通用）", async () => {
		const saved: Settings[] = [];
		const store = fakeSettingsStore(baseSettings, saved);
		const next = await saveServerAddress(
			{ store, client: () => fakeAuthClient({ token: "" }) },
			baseSettings,
			" https://other.example.com/ ",
		);
		expect(next).toEqual({
			baseUrl: "https://other.example.com",
			email: "",
			sessionToken: "",
			notifications: true,
			deviceID: undefined,
		});
		expect(saved[0]?.baseUrl).toBe("https://other.example.com");
	});

	it("地址相同则保留登录态", async () => {
		const next = await saveServerAddress(
			{ store: fakeSettingsStore(baseSettings, []), client: () => fakeAuthClient({ token: "" }) },
			baseSettings,
			baseSettings.baseUrl,
		);
		expect(next.sessionToken).toBe(baseSettings.sessionToken);
	});
});

describe("canRegister", () => {
	it("closed 时隐藏注册入口，其余情况保留", () => {
		expect(canRegister(null)).toBe(true);
		expect(canRegister({ initialized: true, registration_mode: "open" })).toBe(true);
		expect(canRegister({ initialized: true, registration_mode: "closed" })).toBe(false);
	});
});
