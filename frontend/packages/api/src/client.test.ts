import { afterEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "./client";

const fetchMock = vi.fn();

afterEach(() => {
	vi.unstubAllGlobals();
	vi.clearAllMocks();
});

function stubFetch(response: Response | (() => Response)) {
	vi.stubGlobal("fetch", fetchMock);
	fetchMock.mockImplementation(() =>
		Promise.resolve(typeof response === "function" ? response() : response),
	);
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

	it("listPendingPermissions 归一化 agent 快照并编码路径", async () => {
		stubFetch(
			new Response(
				JSON.stringify([
					{ id: "perm_1", sessionID: "ses 1", action: "bash", resources: ["rm -rf"] },
				]),
				{ status: 200 },
			),
		);
		const api = createApiClient();
		const permissions = await api.listPendingPermissions("node/1", "ses 1");
		expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/sessions/node%2F1/ses%201/permissions");
		expect(permissions).toEqual([
			{
				id: "perm_1",
				session_id: "ses 1",
				action: "bash",
				resources: ["rm -rf"],
				message: undefined,
			},
		]);
	});

	it("listMessages 透传分页游标并返回整页", async () => {
		stubFetch(
			new Response(JSON.stringify({ messages: [{ id: "m1" }], next: "older" }), { status: 200 }),
		);
		const api = createApiClient();
		const page = await api.listMessages("n1", "s1", "cursor 1", 50);
		expect(fetchMock.mock.calls[0]?.[0]).toBe(
			"/api/sessions/n1/s1/messages?cursor=cursor+1&limit=50",
		);
		expect(page.next).toBe("older");
	});

	it("listMessages 兼容老控制面的数组响应", async () => {
		stubFetch(new Response(JSON.stringify([{ id: "m1" }]), { status: 200 }));
		const api = createApiClient();
		const page = await api.listMessages("n1", "s1");
		expect(page.messages).toEqual([{ id: "m1" }]);
	});

	it("listChildSessions 按父会话查询并带目录", async () => {
		stubFetch(new Response(JSON.stringify([{ id: "ses_child" }]), { status: 200 }));
		const api = createApiClient();
		const children = await api.listChildSessions("n1", "ses_parent", "/work/a b");
		expect(fetchMock.mock.calls[0]?.[0]).toBe(
			"/api/sessions/n1/ses_parent/children?directory=%2Fwork%2Fa+b",
		);
		expect(children).toEqual([{ id: "ses_child" }]);
	});

	it("listChildSessions 省略空目录", async () => {
		stubFetch(new Response("[]", { status: 200 }));
		const api = createApiClient();
		await api.listChildSessions("n1", "ses_parent");
		expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/sessions/n1/ses_parent/children");
	});

	it("getNodeHealth 把导出字段转成小写形状", async () => {
		stubFetch(
			new Response(
				JSON.stringify({ Running: true, Mode: "managed", BaseURL: "http://127.0.0.1:4096" }),
				{
					status: 200,
				},
			),
		);
		const api = createApiClient();
		const health = await api.getNodeHealth("node/1");
		expect(health).toEqual({
			running: true,
			mode: "managed",
			base_url: "http://127.0.0.1:4096",
		});
	});

	it("listPermissions 直接返回 agent 快照数组", async () => {
		stubFetch(
			new Response(JSON.stringify([{ id: "perm_1", sessionID: "s1", action: "bash" }]), {
				status: 200,
			}),
		);
		const api = createApiClient();
		const permissions = await api.listPermissions("n1", "s1");
		expect(permissions[0]?.sessionID).toBe("s1");
	});

	it("registerDevice 提交 snake_case 并返回设备", async () => {
		stubFetch(new Response(JSON.stringify({ id: "device_1" }), { status: 201 }));
		const api = createApiClient();
		const device = await api.registerDevice({
			name: "pixel",
			platform: "android",
			push_token: "tok",
		});
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

describe("project aggregation", () => {
	it("listProjects 解包 projects 信封", async () => {
		stubFetch(
			new Response(
				JSON.stringify({
					projects: [
						{
							key: "abc",
							id: "abc",
							canonical: "/w/repo",
							nodes: [{ node_id: "n1", online: true, last_seen: "2026-11-12T00:00:00Z" }],
							last_seen: "2026-11-12T00:00:00Z",
						},
					],
				}),
				{ status: 200 },
			),
		);
		const api = createApiClient();
		const projects = await api.listProjects();
		expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/projects");
		expect(projects).toHaveLength(1);
		expect(projects[0]?.key).toBe("abc");
	});

	it("listProjects 缺少信封时返回空数组", async () => {
		stubFetch(new Response(JSON.stringify({}), { status: 200 }));
		const api = createApiClient();
		await expect(api.listProjects()).resolves.toEqual([]);
	});

	it("createProject 提交名字并返回 canonical", async () => {
		stubFetch(new Response(JSON.stringify({ canonical: "/w/新项目" }), { status: 200 }));
		const api = createApiClient();
		await expect(api.createProject("node 1", "新项目")).resolves.toEqual({
			canonical: "/w/新项目",
		});
		expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/nodes/node%201/projects");
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(init.method).toBe("POST");
		expect(JSON.parse(String(init.body))).toEqual({ name: "新项目" });
	});

	it("setProjectPrimaryNode 编码含冒号与斜杠的 key", async () => {
		stubFetch(new Response(null, { status: 204 }));
		const api = createApiClient();
		await api.setProjectPrimaryNode("global:/w/repo", "n1");
		expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/projects/global%3A%2Fw%2Frepo/primary-node");
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(init.method).toBe("PUT");
		expect(JSON.parse(String(init.body))).toEqual({ node_id: "n1" });
	});

	it("getPreferences 返回 values 映射", async () => {
		stubFetch(
			new Response(JSON.stringify({ values: { "inbox.dir.n1": "/w/inbox" } }), { status: 200 }),
		);
		const api = createApiClient();
		await expect(api.getPreferences()).resolves.toEqual({ "inbox.dir.n1": "/w/inbox" });
	});

	it("setPreference 编码 key 并提交 value", async () => {
		stubFetch(new Response(null, { status: 204 }));
		const api = createApiClient();
		await api.setPreference("inbox.dir.n 1", "/w/inbox");
		expect(fetchMock.mock.calls[0]?.[0]).toBe("/api/preferences/inbox.dir.n%201");
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(JSON.parse(String(init.body))).toEqual({ value: "/w/inbox" });
	});
});

describe("认证、认领与移除节点", () => {
	it("login 提交 email/password 并返回会话令牌", async () => {
		stubFetch(
			new Response(JSON.stringify({ user: { id: "u1", email: "a@example.com" }, token: "sess" }), {
				status: 200,
			}),
		);
		const api = createApiClient({ baseUrl: "http://box:8787" });
		const body = await api.login("a@example.com", "password123");

		expect(String(fetchMock.mock.calls[0]?.[0])).toBe("http://box:8787/api/auth/login");
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(init.method).toBe("POST");
		expect(JSON.parse(String(init.body))).toEqual({
			email: "a@example.com",
			password: "password123",
		});
		expect(body.token).toBe("sess");
	});

	it("getAuthMethods 不带鉴权头", async () => {
		stubFetch(
			new Response(JSON.stringify({ initialized: true, registration_mode: "open" }), {
				status: 200,
			}),
		);
		await createApiClient({ baseUrl: "http://box:8787" }).getAuthMethods();
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(init.headers).not.toMatchObject({ Authorization: expect.anything() });
	});

	it("claimPreview / claimConfirm 使用 /api/claim/{code}", async () => {
		// 两次请求各要一个 Response：body 只能读一次，复用同一个实例会报
		// "Body has already been read"。
		stubFetch(
			() => new Response(JSON.stringify({ code: "AB234", node_name: "box" }), { status: 200 }),
		);
		const api = createApiClient({ baseUrl: "http://box:8787" });
		await api.claimPreview("ab234");
		expect(String(fetchMock.mock.calls[0]?.[0])).toBe("http://box:8787/api/claim/ab234");
		await api.claimConfirm("ab234");
		const init = fetchMock.mock.calls[1]?.[1] as RequestInit;
		expect(init.method).toBe("POST");
	});

	it("deleteNode 发 DELETE 并接受 204", async () => {
		stubFetch(new Response(null, { status: 204 }));
		await expect(
			createApiClient({ baseUrl: "http://box:8787" }).deleteNode("node_1"),
		).resolves.toBeNull();
		expect(String(fetchMock.mock.calls[0]?.[0])).toBe("http://box:8787/api/nodes/node_1");
		expect((fetchMock.mock.calls[0]?.[1] as RequestInit).method).toBe("DELETE");
	});

	it("renameNode 用 PATCH 提交展示名", async () => {
		stubFetch(new Response(JSON.stringify({ id: "node_1", name: "客厅的电脑" }), { status: 200 }));
		const view = await createApiClient({ baseUrl: "http://box:8787" }).renameNode(
			"node 1",
			"客厅的电脑",
		);

		expect(String(fetchMock.mock.calls[0]?.[0])).toBe("http://box:8787/api/nodes/node%201");
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(init.method).toBe("PATCH");
		expect(JSON.parse(String(init.body))).toEqual({ name: "客厅的电脑" });
		expect(view.name).toBe("客厅的电脑");
	});

	it("setNodeColor 用 PATCH 提交颜色", async () => {
		stubFetch(new Response(JSON.stringify({ id: "node_1", color: "blue" }), { status: 200 }));
		const view = await createApiClient({ baseUrl: "http://box:8787" }).setNodeColor(
			"node 1",
			"blue",
		);

		expect(String(fetchMock.mock.calls[0]?.[0])).toBe("http://box:8787/api/nodes/node%201");
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(init.method).toBe("PATCH");
		expect(JSON.parse(String(init.body))).toEqual({ color: "blue" });
		expect(view.color).toBe("blue");
	});

	it("createClaim 提交节点名并返回短期码", async () => {
		stubFetch(
			new Response(JSON.stringify({ code: "AB234", node_name: "box", expires_at: "t" }), {
				status: 200,
			}),
		);
		const claim = await createApiClient({ baseUrl: "http://box:8787" }).createClaim("box");

		expect(String(fetchMock.mock.calls[0]?.[0])).toBe("http://box:8787/api/node-claims");
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(init.method).toBe("POST");
		expect(JSON.parse(String(init.body))).toEqual({ node_name: "box" });
		expect(claim.code).toBe("AB234");
	});

	it("createClaim 省略节点名时提交空串而不是 undefined", async () => {
		stubFetch(new Response(JSON.stringify({ code: "AB234" }), { status: 200 }));
		await createApiClient().createClaim();
		const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
		expect(JSON.parse(String(init.body))).toEqual({ node_name: "" });
	});

	it("getNode 与 getNodeProjects 走节点子路径", async () => {
		// 两个请求各要一个 Response，否则第二次会撞上 body 已被读取。
		stubFetch(
			() =>
				new Response(
					JSON.stringify({
						id: "node 1",
						projects: [{ id: "p1", canonical: "/w/repo", vcs: "git" }],
					}),
					{ status: 200 },
				),
		);
		const api = createApiClient({ baseUrl: "http://box:8787" });
		await api.getNode("node 1");
		expect(String(fetchMock.mock.calls[0]?.[0])).toBe("http://box:8787/api/nodes/node%201");

		await expect(api.getNodeProjects("node 1")).resolves.toEqual([
			{ id: "p1", canonical: "/w/repo", vcs: "git" },
		]);
		expect(String(fetchMock.mock.calls[1]?.[0])).toBe(
			"http://box:8787/api/nodes/node%201/projects",
		);
	});

	it("getNodeProjects 缺省 projects 时返回空数组", async () => {
		stubFetch(new Response(JSON.stringify({}), { status: 200 }));
		await expect(createApiClient().getNodeProjects("n1")).resolves.toEqual([]);
	});

	it("401 触发 onUnauthorized 钩子并照常抛错", async () => {
		stubFetch(new Response(JSON.stringify({ code: "unauthorized" }), { status: 401 }));
		const onUnauthorized = vi.fn();
		const api = createApiClient({ baseUrl: "http://box:8787", onUnauthorized });
		await expect(api.listNodes()).rejects.toMatchObject({ status: 401 });
		expect(onUnauthorized).toHaveBeenCalledTimes(1);
	});
});
