// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
	connection: {
		device: { id: "spark-1", name: "办公室 Spark", management: true },
		status: "online",
		baseURL: "http://spark.local:38081/v1",
	} as {
		device: { id: string; name: string; management: boolean };
		status: "online" | "offline";
		baseURL: string;
	} | null,
	status: vi.fn(),
	workflowCurrent: vi.fn(),
	workflowGet: vi.fn(),
	workflowBegin: vi.fn(),
	preflight: vi.fn(),
	changeMaintenance: vi.fn(),
	changeModel: vi.fn(),
	user: { publicId: "user", currentOrg: { id: 7, isAdmin: true } } as {
		publicId: string;
		currentOrg: { id: number; isAdmin: boolean };
	} | null,
}));
vi.mock("@leros/store", () => ({
	sparkWorkflowApi: {
		current: mock.workflowCurrent,
		get: mock.workflowGet,
		begin: mock.workflowBegin,
	},
	sparkApi: {
		status: mock.status,
		preflight: mock.preflight,
		changeMaintenance: mock.changeMaintenance,
		changeModel: mock.changeModel,
	},
	API_BASE_URL: "http://spark.local:38081/v1",
	useAuthStore: (select: (s: unknown) => unknown) => select({ authUser: mock.user }),
}));

vi.mock("./SparkConnectionGate", () => ({
	useSparkConnection: () => mock.connection,
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
				{
					kind: "docker",
					id: "one",
					name: "old-model",
					state: "exited",
					registered: false,
				},
			],
		},
		workers: [
			{
				worker_id: 9,
				state: "unknown",
				running_count: null,
				waiting_count: null,
			},
		],
		checks: [{ code: "execution_disabled", message: "尚未启用执行能力" }],
		can_shutdown: false,
		can_start_model: false,
		can_stop_model: false,
	};
}
beforeEach(() => {
	vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
	vi.resetAllMocks();
	mock.workflowCurrent.mockResolvedValue({
		enabled: false,
		current: null,
		can_start: false,
		can_stop: false,
	});
	mock.connection = {
		device: { id: "spark-1", name: "办公室 Spark", management: true },
		status: "online",
		baseURL: "http://spark.local:38081/v1",
	};
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
	mock.user = { publicId: "user", currentOrg: { id: 7, isAdmin: false } };
	await render();
	expect(mock.status).not.toHaveBeenCalled();
	expect(container.textContent).toContain("管理员");
	expect(container.textContent).toContain("办公室 Spark");
	expect(container.querySelector("button")).toBeNull();
	mock.user.currentOrg.isAdmin = true;
	await render();
	expect(mock.status).toHaveBeenCalledOnce();
	expect(container.textContent).toContain("工作负载 1");
	expect(container.textContent).toContain("状态未知");
	for (const label of ["启动本机模型", "停止本机模型"]) expect(button(label).disabled).toBe(true);
	expect(mock.preflight).not.toHaveBeenCalled();
	expect(container.textContent).not.toContain("关机");
	expect(container.textContent).not.toContain("可以拔电");
});
it("old backend and failed refresh clear prior status", async () => {
	await render();
	expect(container.textContent).toContain("工作负载 1");
	mock.status.mockRejectedValue(new Error("当前后端版本尚不支持 Spark 设备管理。"));
	await act(async () => button("刷新状态").click());
	expect(container.textContent).toContain("当前后端版本尚不支持");
	expect(container.textContent).not.toContain("工作负载 1");
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
	expect(container.textContent).not.toContain("工作负载 1");
	mock.user = null;
	await render();
	expect(container.textContent).not.toContain("工作负载 1");
});
it("expires observations instead of retaining old management authority", async () => {
	vi.useFakeTimers();
	try {
		mock.status.mockResolvedValue(managedModel());
		await render();
		await act(async () => button("启动本机模型").click());
		await act(async () => vi.advanceTimersByTime(31000));
		expect(container.textContent).toContain("已过期");
		expect(container.textContent).not.toContain("工作负载 1");
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
		expect(container.textContent).not.toContain("工作负载 1");
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
	expect(container.textContent).toContain("服务端：维护中");
	expect(container.textContent).toContain("Worker 服务：维护中");
	expect(container.textContent).not.toContain("关机");
	expect(container.textContent).not.toContain("可以拔电");
});

it("requires confirmation and waits for consumer agreement after maintenance write", async () => {
	const base = {
		...snapshot(),
		maintenance: "accepting",
		maintenance_control: {
			revision: 3,
			operation_id: "old",
			active: false,
			confirmed: true,
			can_pause: true,
			can_resume: false,
		},
	};
	mock.status.mockResolvedValue(base);
	mock.changeMaintenance.mockResolvedValue({
		revision: 4,
		operation_id: "new",
		active: true,
	});
	await render();
	await act(async () => button("进入维护模式").click());
	expect(mock.changeMaintenance).not.toHaveBeenCalled();
	expect(container.textContent).toContain("正在运行的任务");
	mock.status.mockResolvedValue({
		...base,
		maintenance_control: {
			revision: 4,
			operation_id: "new",
			active: true,
			confirmed: false,
			can_pause: false,
			can_resume: false,
		},
	});
	await act(async () => button("确认进入维护").click());
	expect(mock.changeMaintenance).toHaveBeenCalledOnce();
	expect(container.textContent).toContain("等待服务端和 Worker 确认");
	expect(button("恢复服务").disabled).toBe(true);
	expect(container.textContent).not.toContain("关机");
});

it("clears mutation authority on unknown outcome and requires a fresh read", async () => {
	mock.status.mockResolvedValue({
		...snapshot(),
		maintenance_control: {
			revision: 3,
			operation_id: "op",
			active: true,
			confirmed: true,
			can_pause: false,
			can_resume: true,
		},
	});
	mock.changeMaintenance.mockRejectedValue(
		new Error("操作结果尚未确认，请刷新状态核对；不要重复提交。"),
	);
	await render();
	await act(async () => button("恢复服务").click());
	await act(async () => button("确认恢复服务").click());
	expect(container.textContent).toContain("结果尚未确认");
	expect(container.textContent).not.toContain("任务服务：正常服务");
	expect(mock.changeMaintenance).toHaveBeenCalledOnce();
	expect(container.querySelector('[role="alertdialog"]')).toBeNull();
	expect(button("刷新状态").disabled).toBe(false);
});

function managedModel() {
	return {
		...snapshot(),
		maintenance: "paused",
		maintenance_control: {
			revision: 3,
			operation_id: "maintenance",
			active: true,
			confirmed: true,
			can_pause: false,
			can_resume: true,
		},
		workers: [
			{
				worker_id: 9,
				state: "observed",
				running_count: 0,
				waiting_count: 0,
				maintenance: "paused",
			},
		],
		model_control: {
			label: "Local Qwen",
			state: "stopped",
			revision: 2,
			action: "none",
			error_code: "",
			busy: false,
			can_start: true,
			can_stop: false,
		},
		can_start_model: true,
	};
}
it("confirms a registered model start once and reads actual progress instead of assuming ready", async () => {
	const initial = managedModel();
	mock.status.mockResolvedValue(initial);
	mock.changeModel.mockResolvedValue({ state: "ready" });
	await render();
	expect(button("启动本机模型").disabled).toBe(false);
	await act(async () => button("启动本机模型").click());
	expect(container.querySelector('[role="alertdialog"]')?.textContent).toContain("Local Qwen");
	expect(container.querySelector('[role="alertdialog"]')?.textContent).toContain("本机模型");
	expect(mock.changeModel).not.toHaveBeenCalled();
	mock.status.mockResolvedValue({
		...initial,
		model_control: {
			...initial.model_control,
			state: "starting",
			action: "start",
			busy: true,
			can_start: false,
		},
		can_start_model: false,
	});
	const confirm = button("确认启动模型");
	await act(async () => {
		confirm.click();
		confirm.click();
	});
	expect(mock.changeModel).toHaveBeenCalledExactlyOnceWith({
		action: "start",
		boot_id: "boot",
		expected_revision: 2,
		maintenance_revision: 3,
		operation_id: expect.any(String),
	});
	expect(container.textContent).toContain("启动中");
	expect(container.textContent).not.toContain("已就绪");
	expect(button("恢复服务").disabled).toBe(true);
	expect(container.textContent).not.toContain("关机");
});
it("polls busy models serially and stops polling after organization change", async () => {
	vi.useFakeTimers();
	try {
		const data = managedModel();
		mock.status.mockResolvedValue({
			...data,
			model_control: {
				...data.model_control,
				state: "starting",
				busy: true,
				can_start: false,
			},
		});
		await render();
		const visibleStatus = container.textContent;
		let resolve: (value: unknown) => void = () => {
			/* 请求发起时替换。 */
		};
		mock.status.mockImplementationOnce(
			() =>
				new Promise((r) => {
					resolve = r;
				}),
		);
		await act(async () => vi.advanceTimersByTime(2000));
		expect(mock.status).toHaveBeenCalledTimes(2);
		expect(container.textContent).toBe(visibleStatus);
		expect(container.textContent).not.toContain("正在核对设备状态");
		await act(async () => vi.advanceTimersByTime(6000));
		expect(mock.status).toHaveBeenCalledTimes(2);
		mock.user = null;
		await render();
		await act(async () => resolve(data));
		await act(async () => vi.advanceTimersByTime(10000));
		expect(mock.status).toHaveBeenCalledTimes(2);
		expect(container.textContent).not.toContain("Local Qwen");
	} finally {
		vi.useRealTimers();
	}
});
it("shows stop failures and retains a manual status refresh with no automatic mutation replay", async () => {
	const data = managedModel();
	mock.status.mockResolvedValue({
		...data,
		can_start_model: false,
		can_stop_model: true,
		model_control: {
			...data.model_control,
			state: "ready",
			can_start: false,
			can_stop: true,
		},
	});
	mock.changeModel.mockRejectedValue(
		new Error("模型操作结果尚未确认，请刷新状态核对；不要重复提交。"),
	);
	await render();
	await act(async () => button("停止本机模型").click());
	expect(container.querySelector('[role="alertdialog"]')?.textContent).toContain("等待");
	mock.status.mockResolvedValue({
		...data,
		can_start_model: false,
		model_control: {
			...data.model_control,
			state: "unknown",
			can_start: false,
			error_code: "gate_unavailable",
		},
	});
	await act(async () => button("确认停止模型").click());
	expect(mock.changeModel).toHaveBeenCalledOnce();
	expect(container.textContent).toContain("结果尚未确认");
	expect(button("刷新状态").disabled).toBe(false);
	await act(async () => button("刷新状态").click());
	expect(container.textContent).toContain("gate_unavailable");
	expect(button("启动本机模型").disabled).toBe(true);
	expect(button("停止本机模型").disabled).toBe(true);
});
it("requires entered maintenance and idle workers even if a model capability is advertised", async () => {
	for (const patch of [
		{ maintenance: "accepting" },
		{
			workers: [{ worker_id: 9, state: "busy", running_count: 0, waiting_count: 0 }],
		},
		{ maintenance_control: undefined },
	]) {
		mock.status.mockResolvedValue({ ...managedModel(), ...patch });
		await render();
		await act(async () => button("刷新状态").click());
		expect(button("启动本机模型").disabled).toBe(true);
	}
});

it("loads status after the strict-mode effect remount", async () => {
	await act(async () =>
		root.render(
			<StrictMode>
				<SparkManagementCard />
			</StrictMode>,
		),
	);
	expect(container.textContent).toContain("工作负载 1");
	expect(button("刷新状态").disabled).toBe(false);
});

it("does not submit an old confirmation once a status refresh has begun", async () => {
	mock.status.mockResolvedValue(managedModel());
	await render();
	await act(async () => button("启动本机模型").click());
	const confirm = button("确认启动模型");
	mock.status.mockImplementationOnce(
		() =>
			new Promise(() => {
				/* 保持读取进行中。 */
			}),
	);
	await act(async () => {
		button("刷新状态").click();
		confirm.click();
	});
	expect(mock.changeModel).not.toHaveBeenCalled();
});

it("hides an unrecognized connection without administrator requests", async () => {
	mock.connection = null;
	await render();
	expect(container.textContent).toBe("");
	expect(mock.status).not.toHaveBeenCalled();
});
it.each([
	"offline",
	"unmanaged",
])("shows known %s device without querying admin APIs", async (mode) => {
	if (!mock.connection) throw new Error("fixture missing");
	if (mode === "offline") mock.connection.status = "offline";
	else mock.connection.device.management = false;
	await render();
	expect(container.textContent).toContain("办公室 Spark");
	expect(container.textContent).toContain(mode === "offline" ? "离线" : "尚未启用设备管理");
	expect(mock.status).not.toHaveBeenCalled();
	expect(container.querySelector("button")).toBeNull();
});
it("keeps model readiness and task admission separate with actionable legacy guidance", async () => {
	const data = managedModel();
	mock.status.mockResolvedValue({
		...data,
		model_control: {
			...data.model_control,
			state: "ready",
			can_start: false,
			can_stop: true,
		},
		can_start_model: false,
		can_stop_model: true,
	});
	await render();
	expect(container.textContent).toContain("已就绪");
	expect(container.textContent).toContain("新任务：已暂停");
	expect(container.textContent).toContain("不会自动恢复");
	mock.status.mockResolvedValue({
		...data,
		maintenance: "accepting",
		maintenance_control: {
			...data.maintenance_control,
			active: false,
			can_pause: true,
			can_resume: false,
		},
		can_start_model: false,
		model_control: { ...data.model_control, can_start: false },
	});
	await act(async () => button("刷新状态").click());
	expect(container.textContent).toContain("新任务：允许提交");
	expect(container.textContent).toContain("已停止");
	expect(container.textContent).toContain("先暂停新任务");
	expect(mock.changeMaintenance).not.toHaveBeenCalled();
});
it("collapses technical details and maintenance controls with no preflight or shutdown entry", async () => {
	mock.status.mockResolvedValue(managedModel());
	await render();
	const diagnostics = [...container.querySelectorAll("details")].find((d) =>
		d.querySelector("summary")?.textContent?.includes("诊断"),
	);
	expect(diagnostics).toBeDefined();
	expect(diagnostics?.open).toBe(false);
	expect(diagnostics?.textContent).toContain("工作负载 1");
	const advanced = button("恢复服务").closest("details");
	expect(advanced?.querySelector("summary")?.textContent).toContain("高级维护");
	expect(advanced?.open).toBe(false);
	expect(container.textContent).not.toContain("关机");
});
it("never displays arbitrary remote failures or check messages", async () => {
	mock.status.mockRejectedValueOnce(new Error("REMOTE_SECRET Authorization: Bearer token"));
	await render();
	expect(container.textContent).not.toContain("REMOTE_SECRET");
	expect(container.textContent).toContain("检查连接");
	const data = managedModel();
	mock.status.mockResolvedValue({
		...data,
		model_control: { ...data.model_control, error_code: "ERROR_SECRET" },
		checks: [{ code: "CODE_SECRET", message: "CHECK_SECRET" }],
	});
	await act(async () => button("刷新状态").click());
	expect(container.textContent).not.toMatch(/ERROR_SECRET|CODE_SECRET|CHECK_SECRET/);
});
it("copies only a safe diagnostic projection and offers selectable text on clipboard denial", async () => {
	const data = managedModel();
	mock.status.mockResolvedValue({
		...data,
		snapshot: {
			...data.snapshot,
			hostname: "HOST_SECRET",
			host_id: "HOST_ID_SECRET",
			boot_id: "BOOT_SECRET",
			components: [
				{
					kind: "docker",
					id: "ID_SECRET",
					name: "NAME_SECRET",
					state: "STATE_SECRET",
					registered: false,
					env: "ENV_SECRET",
				},
			],
		},
		model_control: {
			...data.model_control,
			label: "LABEL_SECRET",
			error_code: "ERROR_SECRET",
			operation_id: "OP_SECRET",
		},
		checks: [{ code: "CHECK_CODE_SECRET", message: "CHECK_SECRET" }],
	});
	const writeText = vi.fn().mockRejectedValue(new Error("CLIPBOARD_SECRET"));
	Object.defineProperty(navigator, "clipboard", {
		configurable: true,
		value: { writeText },
	});
	await render();
	await act(async () => button("复制诊断").click());
	const diagnostics = button("复制诊断").closest("details");
	if (!diagnostics) throw new Error("missing diagnostics");
	diagnostics.open = true;
	expect(diagnostics.textContent).not.toMatch(/SECRET|spark.local|token|Authorization|jwt/i);
	expect(diagnostics.textContent).toContain("工作负载 1");
	expect(diagnostics.textContent).toContain("状态未知");
	expect(writeText).toHaveBeenCalledOnce();
	const copied = writeText.mock.calls[0][0] as string;
	expect(copied).not.toMatch(/SECRET|spark.local|token|Authorization|jwt/i);
	expect(JSON.parse(copied)).toMatchObject({
		connection: "online",
		model: { state: "stopped" },
		task_admission: "paused",
	});
	expect(container.textContent).toContain("复制失败");
	expect(container.querySelector("textarea")?.value).toBe(copied);
	expect(container.querySelector("textarea")?.readOnly).toBe(true);
	expect(container.textContent).not.toContain("CLIPBOARD_SECRET");
});
it("clears pending model authority when the connection becomes offline or changes device", async () => {
	mock.status.mockResolvedValue(managedModel());
	await render();
	await act(async () => button("启动本机模型").click());
	const confirm = button("确认启动模型");
	if (!mock.connection) throw new Error("fixture missing");
	mock.connection = { ...mock.connection, status: "offline" };
	await render();
	await act(async () => confirm.click());
	expect(mock.changeModel).not.toHaveBeenCalled();
	expect(container.textContent).not.toContain("Local Qwen");
	expect(container.textContent).toContain("离线");
	mock.status.mockImplementationOnce(
		() =>
			new Promise(() => {
				/* 保持请求在途，验证并发与身份隔离。 */
			}),
	);
	mock.connection = {
		...mock.connection,
		status: "online",
		device: { ...mock.connection.device, id: "spark-2", name: "新设备" },
	};
	await render();
	expect(container.textContent).toContain("新设备");
	expect(container.textContent).not.toContain("Local Qwen");
});

it("does not refresh or restore old model progress when a submitted operation finishes offline", async () => {
	mock.status.mockResolvedValue(managedModel());
	let finish: (value: unknown) => void = () => {
		/* 请求发起时替换。 */
	};
	mock.changeModel.mockImplementationOnce(
		() =>
			new Promise((resolve) => {
				finish = resolve;
			}),
	);
	await render();
	await act(async () => button("启动本机模型").click());
	await act(async () => button("确认启动模型").click());
	if (!mock.connection) throw new Error("fixture missing");
	mock.connection = { ...mock.connection, status: "offline" };
	await render();
	await act(async () => finish({ state: "ready" }));
	expect(mock.status).toHaveBeenCalledOnce();
	expect(container.textContent).toContain("离线");
	expect(container.textContent).not.toContain("已就绪");
	expect(mock.changeMaintenance).not.toHaveBeenCalled();
});

it.each([
	"model",
	"maintenance",
])("sanitizes rejected %s writes while keeping manual recovery", async (kind) => {
	mock.status.mockResolvedValue(managedModel());
	mock.changeModel.mockRejectedValueOnce(new Error("MUTATION_SECRET token=private"));
	mock.changeMaintenance.mockRejectedValueOnce(new Error("MUTATION_SECRET token=private"));
	await render();
	await act(async () => button(kind === "model" ? "启动本机模型" : "恢复服务").click());
	await act(async () => button(kind === "model" ? "确认启动模型" : "确认恢复服务").click());
	expect(container.textContent).not.toContain("MUTATION_SECRET");
	expect(container.textContent).toContain("结果尚未确认");
	expect(button("刷新状态").disabled).toBe(false);
});

it("refreshes idle visible status periodically without overlapping requests", async () => {
	vi.useFakeTimers();
	try {
		mock.status.mockImplementation(async () => managedModel());
		await render();
		await act(async () => vi.advanceTimersByTime(15000));
		expect(mock.status).toHaveBeenCalledTimes(2);
		expect(container.textContent).toContain("Local Qwen");
		mock.status.mockImplementationOnce(
			() =>
				new Promise(() => {
					/* 保持请求在途，验证并发与身份隔离。 */
				}),
		);
		await act(async () => vi.advanceTimersByTime(15000));
		await act(async () => vi.advanceTimersByTime(30000));
		expect(mock.status).toHaveBeenCalledTimes(3);
		expect(button("启动本机模型").disabled).toBe(true);
	} finally {
		vi.useRealTimers();
	}
});

it("does not interrupt confirmation with polling or focus and still expires its authority", async () => {
	vi.useFakeTimers();
	try {
		mock.status.mockResolvedValue(managedModel());
		await render();
		await act(async () => button("启动本机模型").click());
		await act(async () => vi.advanceTimersByTime(16000));
		await act(async () => window.dispatchEvent(new Event("focus")));
		expect(mock.status).toHaveBeenCalledOnce();
		expect(container.querySelector('[role="alertdialog"]')).not.toBeNull();
		await act(async () => vi.advanceTimersByTime(15000));
		expect(container.querySelector('[role="alertdialog"]')).toBeNull();
		expect(button("启动本机模型").disabled).toBe(true);
		expect(container.textContent).toContain("已过期");
	} finally {
		vi.useRealTimers();
	}
});

it("rechecks on focus and network recovery without polling a pending write", async () => {
	mock.status.mockResolvedValue(managedModel());
	await render();
	await act(async () => window.dispatchEvent(new Event("focus")));
	expect(mock.status).toHaveBeenCalledTimes(2);
	await act(async () => window.dispatchEvent(new Event("online")));
	expect(mock.status).toHaveBeenCalledTimes(3);
	mock.changeModel.mockImplementationOnce(
		() =>
			new Promise(() => {
				/* 保持请求在途，验证并发与身份隔离。 */
			}),
	);
	await act(async () => button("启动本机模型").click());
	await act(async () => button("确认启动模型").click());
	await act(async () => window.dispatchEvent(new Event("focus")));
	expect(mock.status).toHaveBeenCalledTimes(3);
});

it("keeps expanded diagnostics and the manual copy fallback across a periodic refresh", async () => {
	vi.useFakeTimers();
	try {
		mock.status.mockImplementation(async () => managedModel());
		Object.defineProperty(navigator, "clipboard", {
			configurable: true,
			value: { writeText: vi.fn().mockRejectedValue(new Error("denied")) },
		});
		await render();
		const details = button("复制诊断").closest("details");
		if (!details) throw new Error("missing diagnostics");
		details.open = true;
		await act(async () => button("复制诊断").click());
		const field = container.querySelector("textarea");
		const initial = JSON.parse(field?.value ?? "{}");
		let finish: (value: unknown) => void = () => {
			/* 请求发起时替换。 */
		};
		mock.status.mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finish = resolve;
				}),
		);
		await act(async () => vi.advanceTimersByTime(15000));
		expect(button("复制诊断").closest("details")).toBe(details);
		expect(details.open).toBe(true);
		expect(container.querySelector("textarea")).toBe(field);
		expect(button("启动本机模型").disabled).toBe(true);
		const next = managedModel();
		next.snapshot.components[0].state = "running";
		await act(async () => finish(next));
		expect(button("复制诊断").closest("details")).toBe(details);
		expect(details.open).toBe(true);
		expect(container.querySelector("textarea")).toBe(field);
		expect(JSON.parse(field?.value ?? "{}").observed_at).toBe(initial.observed_at + 15);
		expect(details.textContent).toContain("运行中");
	} finally {
		vi.useRealTimers();
	}
});

it("ignores a late clipboard completion for an older diagnostic snapshot", async () => {
	vi.useFakeTimers();
	try {
		mock.status.mockImplementation(async () => managedModel());
		let finish: () => void = () => {
			/* 点击时替换。 */
		};
		const writeText = vi.fn().mockImplementationOnce(
			() =>
				new Promise<void>((resolve) => {
					finish = resolve;
				}),
		);
		Object.defineProperty(navigator, "clipboard", {
			configurable: true,
			value: { writeText },
		});
		await render();
		const details = button("复制诊断").closest("details");
		if (!details) throw new Error("missing diagnostics");
		details.open = true;
		await act(async () => button("复制诊断").click());
		expect(button("复制诊断").disabled).toBe(true);
		await act(async () => vi.advanceTimersByTime(15000));
		await act(async () => finish());
		expect(button("复制诊断").closest("details")).toBe(details);
		expect(container.textContent).not.toContain("已复制诊断");
		expect(button("复制诊断").disabled).toBe(false);
	} finally {
		vi.useRealTimers();
	}
});

it("begins one durable workflow from accepting and recovers persisted progress on reopen", async () => {
	const status = managedModel();
	status.maintenance = "accepting";
	status.maintenance_control.active = false;
	mock.status.mockResolvedValue(status);
	mock.workflowCurrent.mockResolvedValue({
		enabled: true,
		current: null,
		can_start: true,
		can_stop: false,
	});
	const op = {
		operation_id: "op",
		action: "start",
		kind: "workflow",
		phase: "waiting_idle",
		version: 2,
		created_at: 100,
		updated_at: 101,
		deadline: 1300,
		error_code: "",
		needs_attention: false,
		maintenance_retained: true,
	};
	mock.workflowBegin.mockImplementation(async (r) => ({
		...op,
		operation_id: r.operation_id,
	}));
	mock.workflowGet.mockImplementation(async (id) => ({
		...op,
		operation_id: id,
	}));
	await render();
	expect(button("启动本机模型").disabled).toBe(false);
	await act(async () => button("启动本机模型").click());
	await act(async () => button("确认启动模型").click());
	expect(mock.workflowBegin).toHaveBeenCalledTimes(1);
	expect(mock.changeModel).not.toHaveBeenCalled();
	expect(mock.changeMaintenance).not.toHaveBeenCalled();
	mock.workflowCurrent.mockResolvedValue({
		enabled: true,
		current: op,
		can_start: false,
		can_stop: false,
	});
	await act(async () => button("刷新状态").click());
	expect(container.textContent).toContain("等待任务空闲");
	expect(button("启动本机模型").disabled).toBe(true);
});
it("keeps durable attention visible while agent status is unavailable", async () => {
	mock.status.mockRejectedValue(new Error("offline"));
	mock.workflowCurrent.mockResolvedValue({
		enabled: true,
		current: {
			operation_id: "op",
			action: "start",
			kind: "workflow",
			phase: "attention",
			version: 2,
			created_at: 100,
			updated_at: 101,
			deadline: 1300,
			error_code: "outcome_unknown",
			needs_attention: true,
			maintenance_retained: true,
		},
		can_start: false,
		can_stop: false,
	});
	await render();
	expect(container.textContent).toContain("需要管理员核对");
	expect(container.textContent).not.toContain("不支持");
});

it("copies only safe durable progress when the agent is offline", async () => {
	mock.status.mockRejectedValue(new Error("raw-agent-secret"));
	mock.workflowCurrent.mockResolvedValue({
		enabled: true,
		current: {
			operation_id: "op-safe",
			action: "start",
			kind: "workflow",
			phase: "attention",
			version: 2,
			created_at: 100,
			updated_at: 101,
			deadline: 1300,
			error_code: "outcome_unknown",
			needs_attention: true,
			maintenance_retained: false,
			actor: 999,
			host_id: "private-host",
			token: "secret-token",
			raw_error: "private-error",
		},
		can_start: false,
		can_stop: false,
	});
	const writeText = vi.fn().mockResolvedValue(undefined);
	Object.defineProperty(navigator, "clipboard", {
		configurable: true,
		value: { writeText },
	});
	await render();
	await act(async () => button("复制诊断").click());
	const text = writeText.mock.calls[0][0];
	expect(text).toContain("op-safe");
	expect(text).toContain("outcome_unknown");
	expect(text).not.toMatch(/secret|private|999|actor|host_id/);
});

it.each([
	{ action: "pause", phase: "waiting_resume", label: "等待新任务暂停确认" },
	{ action: "pause", phase: "completed_paused", label: "新任务已暂停" },
	{ action: "resume", phase: "completed", label: "新任务提交已恢复" },
])("describes manual maintenance $action/$phase without claiming model readiness", async ({
	action,
	phase,
	label,
}) => {
	const status = managedModel();
	status.model_control.state = "stopped";
	mock.status.mockResolvedValue(status);
	mock.workflowCurrent.mockResolvedValue({
		enabled: true,
		current: {
			operation_id: "manual-op",
			action,
			kind: "manual_maintenance",
			phase,
			version: 3,
			created_at: 100,
			updated_at: 102,
			deadline: 1300,
			error_code: "",
			needs_attention: false,
			maintenance_retained: phase === "completed_paused",
		},
		can_start: false,
		can_stop: false,
	});
	await render();
	const progress = [...container.querySelectorAll("p")].find((p) =>
		p.textContent?.startsWith("操作进度："),
	)?.textContent;
	expect(progress).toContain(label);
	expect(progress).not.toMatch(/模型已就绪|模型操作|等待新任务提交恢复确认/);
	expect(container.textContent).toContain("已停止");
});

it("shows persisted startup elapsed time and stops animation on failure", async () => {
	vi.useFakeTimers();
	try {
		const data = managedModel();
		const starting = {
			...data,
			model_control: { ...data.model_control, state: "starting", busy: true },
		};
		mock.status.mockResolvedValue(starting);
		const op = {
			operation_id: "startup",
			action: "start",
			kind: "workflow",
			phase: "waiting_model",
			version: 2,
			created_at: Math.floor(Date.now() / 1000) - 125,
			updated_at: Math.floor(Date.now() / 1000),
			deadline: Math.floor(Date.now() / 1000) + 1000,
			error_code: "",
			needs_attention: false,
			maintenance_retained: true,
		};
		mock.workflowCurrent.mockResolvedValue({
			enabled: true,
			current: op,
			can_start: false,
			can_stop: false,
		});
		await render();
		expect(container.textContent).toContain("可能需要数分钟");
		expect(container.textContent).toContain("已等待 2 分 05 秒");
		expect(container.querySelector('[class*="motion-safe:animate-spin"]')).not.toBeNull();
		await act(async () => vi.advanceTimersByTime(1000));
		expect(container.textContent).toContain("已等待 2 分 06 秒");
		mock.status.mockResolvedValue({
			...data,
			model_control: {
				...data.model_control,
				state: "error",
				busy: false,
				error_code: "model_start_timeout",
			},
		});
		mock.workflowCurrent.mockResolvedValue({
			enabled: true,
			current: { ...op, phase: "attention", needs_attention: true },
			can_start: false,
			can_stop: false,
		});
		await act(async () => button("刷新状态").click());
		expect(container.querySelector('[class*="motion-safe:animate-spin"]')).toBeNull();
		expect(container.textContent).not.toContain("已等待");
		expect(container.textContent).toContain("模型启动超时");
	} finally {
		vi.useRealTimers();
	}
});
