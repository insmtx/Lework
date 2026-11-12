import { afterEach, describe, expect, it, vi } from "vitest";
import { createClient } from "./client";
import type { Settings } from "../settings/settings";

const fetchMock = vi.fn();

afterEach(() => {
	vi.unstubAllGlobals();
	vi.clearAllMocks();
});

function stubFetch() {
	vi.stubGlobal("fetch", fetchMock);
	fetchMock.mockImplementation(() => Promise.resolve(new Response("[]", { status: 200 })));
}

describe("createClient", () => {
	it("带 Bearer 头且令牌不出现在 URL", async () => {
		stubFetch();
		const settings: Settings = { baseUrl: "http://box:8787", token: "secret-token", notifications: false };
		await createClient(settings).listNodes();

		const url = String(fetchMock.mock.calls[0]?.[0]);
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(url).toBe("http://box:8787/api/nodes");
		expect(url).not.toContain("secret-token");
		expect(init.headers).toMatchObject({ Authorization: "Bearer secret-token" });
	});

	it("每次请求重新读取令牌", async () => {
		stubFetch();
		const settings: Settings = { baseUrl: "http://box:8787", token: "t1", notifications: false };
		const client = createClient(settings);
		await client.listNodes();
		settings.token = "t2";
		await client.listNodes();

		const init = fetchMock.mock.calls[1]?.[1] as RequestInit;
		expect(init.headers).toMatchObject({ Authorization: "Bearer t2" });
	});
});
