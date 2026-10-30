// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
	status: vi.fn(),
	preflight: vi.fn(),
	user: { publicId: "user", currentOrg: { id: 7, isAdmin: true } } as {
		publicId: string;
		currentOrg: { id: number; isAdmin: boolean };
	} | null,
}));
vi.mock("@leros/store", () => ({
	sparkApi: { status: mock.status, preflight: mock.preflight },
	API_BASE_URL: "http://spark.local:38081/v1",
	useAuthStore: (select: (s: unknown) => unknown) => select({ authUser: mock.user }),
}));

import { SparkManagementCard } from "./SparkManagementCard";

let container: HTMLDivElement;
let root: Root;
function snapshot() {
	return {
		valid_until: Math.floor(Date.now() / 1000) + 30,
		snapshot: {
			hostname: "spark",
			host_id: "host",
			boot_id: "boot",
			observed_at: Math.floor(Date.now() / 1000),
			inventory_complete: true,
			components: [
				{ kind: "docker", id: "one", name: "old-model", state: "exited", registered: false },
			],
		},
		workers: [{ worker_id: 9, state: "unknown", running_count: null, waiting_count: null }],
		checks: [{ code: "execution_disabled", message: "尚未启用执行能力" }],
		can_shutdown: false,
		can_start_model: false,
		can_stop_model: false,
	};
}
beforeEach(() => {
	vi.clearAllMocks();
	mock.user = { publicId: "user", currentOrg: { id: 7, isAdmin: true } };
	mock.status.mockResolvedValue(snapshot());
	mock.preflight.mockResolvedValue({
		ready: false,
		checked_at: Math.floor(Date.now() / 1000),
		status: snapshot(),
	});
	container = document.createElement("div");
	document.body.append(container);
	root = createRoot(container);
});
afterEach(() => {
	act(() => root.unmount());
	container.remove();
});
async function render() {
	await act(async () => root.render(<SparkManagementCard />));
}
function button(label: string) {
	const b = [...container.querySelectorAll("button")].find((b) => b.textContent?.includes(label));
	if (!b) throw new Error(`missing ${label}`);
	return b;
}
it("only admins query status and all mutations remain disabled", async () => {
	mock.user!.currentOrg.isAdmin = false;
	await render();
	expect(mock.status).not.toHaveBeenCalled();
	expect(container.textContent).toContain("管理员");
	mock.user!.currentOrg.isAdmin = true;
	await render();
	expect(mock.status).toHaveBeenCalledOnce();
	expect(container.textContent).toContain("old-model");
	expect(container.textContent).toContain("状态未知");
	for (const label of ["启动本机模型", "停止本机模型", "安全关机"])
		expect(button(label).disabled).toBe(true);
	await act(async () => button("关机预检").click());
	expect(mock.preflight).toHaveBeenCalledOnce();
	expect(container.textContent).toContain("暂不满足关机条件");
	expect(container.textContent).not.toContain("可以拔电");
});
it("old backend and failed refresh clear prior status", async () => {
	await render();
	expect(container.textContent).toContain("old-model");
	mock.status.mockRejectedValue(new Error("当前后端版本尚不支持 Spark 设备管理。"));
	await act(async () => button("刷新状态").click());
	expect(container.textContent).toContain("当前后端版本尚不支持");
	expect(container.textContent).not.toContain("old-model");
});
it("discards responses received after organization switch or logout", async () => {
	let resolve: (v: unknown) => void = () => {
		// 渲染时由模拟请求替换，随后用于主动完成该请求。
	};
	mock.status.mockImplementationOnce(
		() =>
			new Promise((r) => {
				resolve = r;
			}),
	);
	await render();
	mock.user = { publicId: "user", currentOrg: { id: 8, isAdmin: false } };
	await render();
	await act(async () => resolve(snapshot()));
	expect(container.textContent).not.toContain("old-model");
	mock.user = null;
	await render();
	expect(container.textContent).not.toContain("old-model");
});
it("expires observations instead of keeping an old preflight result", async () => {
	vi.useFakeTimers();
	try {
		await render();
		await act(async () => vi.advanceTimersByTime(31000));
		expect(container.textContent).toContain("已过期");
		expect(container.textContent).not.toContain("old-model");
	} finally {
		vi.useRealTimers();
	}
});

it("expires at the earliest observation deadline, including workers", async () => {
	vi.useFakeTimers();
	try {
		mock.status.mockResolvedValue({
			...snapshot(),
			valid_until: Math.floor(Date.now() / 1000) + 1,
		});
		await render();
		await act(async () => vi.advanceTimersByTime(2000));
		expect(container.textContent).toContain("已过期");
		expect(container.textContent).not.toContain("old-model");
	} finally {
		vi.useRealTimers();
	}
});

it("distinguishes task admission pause from a powered-off device", async () => {
	mock.status.mockResolvedValue({
		...snapshot(),
		maintenance: "paused",
		workers: [
			{
				worker_id: 9,
				state: "observed",
				running_count: 0,
				waiting_count: 0,
				maintenance: "paused",
			},
		],
	});
	await render();
	expect(container.textContent).toContain("服务端接单：已暂停");
	expect(container.textContent).toContain("Worker 接单：已暂停");
	expect(button("安全关机").disabled).toBe(true);
	expect(container.textContent).not.toContain("可以拔电");
});
