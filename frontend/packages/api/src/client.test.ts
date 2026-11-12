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

describe("control plane additions", () => {
	it("getOverview 解包 nodes", async () => {
		stubFetch(
			new Response(
				JSON.stringify({
					nodes: [
						{
							node_id: "node_1",
							name: "dev-box",
							connected: true,
							sessions: [
								{
									session_id: "ses_1",
									state: "waiting_permission",
									pending_permissions: 1,
									last_activity: "2026-11-12T00:00:00Z",
								},
							],
						},
					],
				}),
				{ status: 200 },
			),
		);
		const api = createApiClient({ baseUrl: "https://cp" });
		const nodes = await api.getOverview();
		expect(fetchMock.mock.calls[0]?.[0]).toBe("https://cp/api/overview");
		expect(nodes).toHaveLength(1);
		expect(nodes[0]?.sessions[0]?.state).toBe("waiting_permission");
	});

	it("listPendingPermissions 解包 permissions 并编码路径", async () => {
		stubFetch(new Response(JSON.stringify({ permissions: [{ id: "perm_1" }] }), { status: 200 }));
		const api = createApiClient();
		const permissions = await api.listPendingPermissions("node/1", "ses 1");
		expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/sessions/node%2F1/ses%201/permissions");
		expect(permissions).toEqual([{ id: "perm_1" }]);
	});

	it("registerDevice 提交 snake_case 并返回设备", async () => {
		stubFetch(new Response(JSON.stringify({ id: "device_1" }), { status: 201 }));
		const api = createApiClient();
		const device = await api.registerDevice({ name: "pixel", platform: "android", push_token: "tok" });
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/devices");
		expect(init.method).toBe("POST");
		expect(JSON.parse(String(init.body))).toEqual({
			name: "pixel",
			platform: "android",
			push_token: "tok",
		});
		expect(device.id).toBe("device_1");
	});

	it("listDevices 与 deleteDevice", async () => {
		stubFetch(() => new Response(JSON.stringify([{ id: "device_1" }]), { status: 200 }));
		const api = createApiClient();
		expect(await api.listDevices()).toHaveLength(1);

		stubFetch(new Response(null, { status: 204 }));
		await expect(api.deleteDevice("device/1")).resolves.toBeNull();
		expect(fetchMock.mock.calls[1]?.[0]).toBe("/api/devices/device%2F1");
	});
});
