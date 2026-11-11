import { afterEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "./client";

const fetchMock = vi.fn();

afterEach(() => {
	vi.unstubAllGlobals();
	vi.clearAllMocks();
});

function stubFetch(response: Response | (() => Response)) {
	vi.stubGlobal("fetch", fetchMock);
	fetchMock.mockImplementation(() => Promise.resolve(typeof response === "function" ? response() : response));
}

describe("createApiClient", () => {
	it("按 baseUrl 拼接路径", async () => {
		stubFetch(new Response("[]", { status: 200 }));
		const api = createApiClient({ baseUrl: "https://cp.example:8787" });
		await api.listNodes();
		expect(fetchMock.mock.calls[0]?.[0]).toBe("https://cp.example:8787/api/nodes");
	});

	it("每次请求都重新求值 headers", async () => {
		stubFetch(() => new Response("[]", { status: 200 }));
		let token = "t1";
		const api = createApiClient({ headers: () => ({ Authorization: `Bearer ${token}` }) });
		await api.listNodes();
		token = "t2";
		await api.listNodes();
		const init = fetchMock.mock.calls[1]?.[1] as RequestInit;
		expect(init.headers).toMatchObject({ Authorization: "Bearer t2" });
	});

	it("把非 2xx 的 JSON 错误映射为 ApiError", async () => {
		stubFetch(
			new Response(JSON.stringify({ code: "node_offline", message: "node is offline" }), {
				status: 503,
			}),
		);
		const api = createApiClient();
		await expect(api.listNodes()).rejects.toMatchObject({
			name: "ApiError",
			status: 503,
			code: "node_offline",
			message: "node is offline",
		});
	});

	it("204 返回 null", async () => {
		stubFetch(new Response(null, { status: 204 }));
		const api = createApiClient();
		await expect(api.closePTY("n1", "s1", "p1")).resolves.toBeNull();
	});
});
