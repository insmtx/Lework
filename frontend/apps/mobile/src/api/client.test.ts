import { afterEach, describe, expect, it, vi } from "vitest";
import { createClient } from "./client";
import { resetSessionExpiredHandlerForTests, setSessionExpiredHandler } from "../settings/session-events";
import type { Settings } from "../settings/settings";

const fetchMock = vi.fn();

afterEach(() => {
	vi.unstubAllGlobals();
	vi.clearAllMocks();
	resetSessionExpiredHandlerForTests();
});

function stubFetch() {
	vi.stubGlobal("fetch", fetchMock);
	fetchMock.mockImplementation(() => Promise.resolve(new Response("[]", { status: 200 })));
}

function stubFetchOnce(response: Response) {
	vi.stubGlobal("fetch", fetchMock);
	fetchMock.mockResolvedValueOnce(response);
}

const signedIn: Settings = {
	baseUrl: "http://box:8787",
	email: "a@example.com",
	sessionToken: "sess-1",
	notifications: false,
};

describe("createClient", () => {
	it("带 Bearer 会话令牌且令牌不出现在 URL", async () => {
		stubFetch();
		const settings: Settings = {
			baseUrl: "http://box:8787",
			email: "a@example.com",
			sessionToken: "secret-token",
			notifications: false,
		};
		await createClient(settings).listNodes();

		const url = String(fetchMock.mock.calls[0]?.[0]);
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(url).toBe("http://box:8787/api/nodes");
		expect(url).not.toContain("secret-token");
		expect(init.headers).toMatchObject({ Authorization: "Bearer secret-token" });
	});

	it("每次请求重新读取令牌", async () => {
		stubFetch();
		const settings: Settings = {
			baseUrl: "http://box:8787",
			email: "a@example.com",
			sessionToken: "t1",
			notifications: false,
		};
		const client = createClient(settings);
		await client.listNodes();
		settings.sessionToken = "t2";
		await client.listNodes();

		const init = fetchMock.mock.calls[1]?.[1] as RequestInit;
		expect(init.headers).toMatchObject({ Authorization: "Bearer t2" });
	});

	it("401 默认通知会话过期，并照常抛错", async () => {
		stubFetchOnce(new Response(JSON.stringify({ code: "unauthorized" }), { status: 401 }));
		const handler = vi.fn();
		setSessionExpiredHandler(handler);
		await expect(createClient(signedIn).listNodes()).rejects.toMatchObject({ status: 401 });
		expect(handler).toHaveBeenCalledTimes(1);
	});

	it("401 invalid_credentials 是输入错误，不触发会话过期通知", async () => {
		// 改密码时旧密码输错，后端也返回 401 invalid_credentials；若当成会话过期
		// 就会把用户踢回登录页，而这恰恰是他正在处理的表单。
		stubFetchOnce(
			new Response(JSON.stringify({ code: "invalid_credentials" }), { status: 401 }),
		);
		const handler = vi.fn();
		setSessionExpiredHandler(handler);
		await expect(createClient(signedIn).listNodes()).rejects.toMatchObject({ status: 401 });
		expect(handler).not.toHaveBeenCalled();
	});

	it("可以注入自定义 onExpired 而不触发全局通知", async () => {
		stubFetchOnce(new Response(JSON.stringify({ code: "unauthorized" }), { status: 401 }));
		const globalHandler = vi.fn();
		const local = vi.fn();
		setSessionExpiredHandler(globalHandler);
		const settings: Settings = {
			baseUrl: "http://box:8787",
			email: "",
			sessionToken: "",
			notifications: false,
		};
		await expect(createClient(settings, local).listNodes()).rejects.toMatchObject({ status: 401 });
		expect(local).toHaveBeenCalledTimes(1);
		expect(globalHandler).not.toHaveBeenCalled();
	});

	it("未登录时仍可构造客户端且不带 Authorization", async () => {
		stubFetch();
		const settings: Settings = {
			baseUrl: "http://box:8787",
			email: "",
			sessionToken: "",
			notifications: false,
		};
		await createClient(settings).listNodes();
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
	});
});
