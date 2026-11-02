import { beforeEach, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
	response: undefined as
		| undefined
		| ((response: Response, context: { url: string; config: RequestInit }) => Promise<Response>),
	token: vi.fn().mockResolvedValue("refreshed"),
}));
vi.mock("@leros/ui/lib/request", () => ({
	createHttpClient: () => ({
		useRequestInterceptor: () => {
			/* 此测试仅验证响应拦截器，不执行请求或升级副作用。 */
		},
		useResponseInterceptor: (handler: typeof mock.response) => {
			mock.response = handler;
		},
	}),
}));
vi.mock("../utils/authStorage", () => ({
	getValidJwtToken: mock.token,
	readStoredAuthUser: () => ({ jwtToken: "old" }),
}));
vi.mock("./config", () => ({ API_BASE_URL: "http://spark.test/v1" }));
vi.mock("./clientUpdatePolicy", () => ({
	dispatchClientUpgradeRequired: () => {
		/* 此测试仅验证响应拦截器，不执行请求或升级副作用。 */
	},
	getClientHeaders: () => ({}),
	readClientUpgradePolicy: () => undefined,
}));
import "./client";

beforeEach(() => {
	mock.token.mockClear();
});

it.each([
	"maintenance",
	"model",
	"workflows",
])("does not replay a %s write after authentication failure", async (endpoint) => {
	const fetch = vi.fn().mockResolvedValue(new Response("ok"));
	vi.stubGlobal("fetch", fetch);
	try {
		const original = new Response("unauthorized", { status: 401 });
		if (!mock.response) throw new Error("response interceptor missing");
		const result = await mock.response(original, {
			url: `http://spark.test/v1/ops/spark/${endpoint}`,
			config: { method: "POST", body: "{}" },
		});
		expect(result).toBe(original);
		expect(result.status).toBe(401);
		expect(fetch).not.toHaveBeenCalled();
		expect(mock.token).not.toHaveBeenCalled();
	} finally {
		vi.unstubAllGlobals();
	}
});

it("refreshes authentication and retries a normal GET after authentication failure", async () => {
	const retried = new Response("ok");
	const fetch = vi.fn().mockResolvedValue(retried);
	vi.stubGlobal("fetch", fetch);
	try {
		const original = new Response("unauthorized", { status: 401 });
		if (!mock.response) throw new Error("response interceptor missing");
		const result = await mock.response(original, {
			url: "http://spark.test/v1/ops/spark/workflows",
			config: { method: "GET", headers: { "X-Test": "preserved" } },
		});
		expect(result).toBe(retried);
		expect(mock.token).toHaveBeenCalledExactlyOnceWith(true);
		expect(fetch).toHaveBeenCalledExactlyOnceWith("http://spark.test/v1/ops/spark/workflows", {
			method: "GET",
			headers: { "X-Test": "preserved", Authorization: "Bearer refreshed" },
		});
	} finally {
		vi.unstubAllGlobals();
	}
});
