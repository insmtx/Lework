import { beforeEach, expect, it, vi } from "vitest";

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("./client", () => ({ apiClient: { get, post } }));

import { sparkApi } from "./sparkApi";

function status() {
	return {
		valid_until: Math.floor(Date.now() / 1000) + 30,
		snapshot: {
			protocol_version: 1,
			host_id: "host",
			boot_id: "boot",
			hostname: "spark",
			observed_at: Math.floor(Date.now() / 1000),
			mode: "read_only",
			model_state: "unconfigured",
			inventory_complete: true,
			components: [],
			checks: [],
		},
		workers: [],
		checks: [{ code: "execution_disabled", message: "仅只读" }],
		can_start_model: false,
		can_stop_model: false,
		can_shutdown: false,
	};
}
beforeEach(() => vi.clearAllMocks());
it("uses only authenticated fixed read-only endpoints and validates envelopes", async () => {
	const data = status();
	get.mockResolvedValue({ data: { code: 0, data } });
	expect(await sparkApi.status()).toEqual(data);
	expect(get).toHaveBeenCalledWith(
		"/ops/spark/status",
		expect.objectContaining({ redirect: "error", retryCount: 0 }),
	);
	post.mockResolvedValue({
		data: {
			code: 0,
			data: { ready: false, checked_at: Math.floor(Date.now() / 1000), status: data },
		},
	});
	expect((await sparkApi.preflight()).ready).toBe(false);
	expect(post).toHaveBeenCalledWith("/ops/spark/preflight", {}, expect.anything());
});
it("rejects missing metadata, stale observations and unexpected execution capabilities", async () => {
	for (const mutate of [
		(s: ReturnType<typeof status>) => {
			s.snapshot.observed_at -= 31;
		},
		(s: ReturnType<typeof status>) => {
			s.can_shutdown = true;
		},
		(s: ReturnType<typeof status>) => {
			s.snapshot.boot_id = "";
		},
		(s: ReturnType<typeof status>) => {
			s.snapshot.protocol_version = 2;
		},
	]) {
		const data = status();
		mutate(data);
		get.mockResolvedValue({ data: { code: 0, data } });
		await expect(sparkApi.status()).rejects.toThrow("状态响应无法验证");
	}
	get.mockResolvedValue({ data: { code: 403, data: status() } });
	await expect(sparkApi.status()).rejects.toThrow();
});
it("reports old backend, forbidden, unavailable and malformed preflight distinctly", async () => {
	for (const [error, message] of [
		[{ status: 404 }, "当前后端版本尚不支持"],
		[{ status: 403 }, "管理员"],
		[{ status: 503, message: "spark_not_configured" }, "尚未配置"],
		[{ status: 502 }, "无法验证"],
	] as const) {
		get.mockRejectedValue(error);
		await expect(sparkApi.status()).rejects.toThrow(message);
	}
	post.mockResolvedValue({
		data: { code: 0, data: { ready: true, checked_at: 0, status: status() } },
	});
	await expect(sparkApi.preflight()).rejects.toThrow("状态响应无法验证");
});

it("rejects responses whose worker evidence expires before the declared deadline", async () => {
	const data = status();
	const worker = {
		worker_id: 9,
		state: "observed",
		observed_at: Math.floor(Date.now() / 1000) - 20,
		running_count: 0,
		waiting_count: 0,
	};
	get.mockResolvedValue({ data: { code: 0, data: { ...data, workers: [worker] } } });
	await expect(sparkApi.status()).rejects.toThrow("状态响应无法验证");
	get.mockResolvedValue({
		data: { code: 0, data: { ...data, valid_until: worker.observed_at + 30, workers: [worker] } },
	});
	expect((await sparkApi.status()).workers[0]?.state).toBe("observed");
	get.mockResolvedValue({
		data: {
			code: 0,
			data: { ...data, workers: [{ ...worker, state: "unknown", observed_at: 0 }] },
		},
	});
	await expect(sparkApi.status()).rejects.toThrow("状态响应无法验证");
});

it("rejects invented maintenance states instead of implying pause confirmation", async () => {
	get.mockResolvedValue({
		data: { code: 0, data: { ...status(), maintenance: "safe_to_unplug" } },
	});
	await expect(sparkApi.status()).rejects.toThrow("状态响应无法验证");
});

it("never treats a malformed maintenance capability as authority to mutate", async () => {
	const base = {
		...status(),
		maintenance: "accepting",
		workers: [
			{
				worker_id: 9,
				state: "observed",
				observed_at: Math.floor(Date.now() / 1000),
				maintenance: "accepting",
				running_count: 0,
				waiting_count: 0,
			},
		],
	};
	for (const control of [
		{
			revision: 1,
			operation_id: "op",
			active: false,
			confirmed: false,
			can_pause: true,
			can_resume: false,
		},
		{
			revision: 1,
			operation_id: "op",
			active: false,
			confirmed: true,
			can_pause: true,
			can_resume: true,
		},
		{
			revision: 0,
			operation_id: "op",
			active: false,
			confirmed: true,
			can_pause: true,
			can_resume: false,
		},
	]) {
		get.mockResolvedValue({ data: { code: 0, data: { ...base, maintenance_control: control } } });
		await expect(sparkApi.status()).rejects.toThrow("无法验证");
	}
});
it("sends one CAS maintenance request and rejects a mismatched receipt", async () => {
	const request = { boot_id: "boot", expected_revision: 3, operation_id: "op", active: true };
	post.mockResolvedValue({
		data: { code: 0, data: { revision: 4, operation_id: "op", active: true } },
	});
	expect(await sparkApi.changeMaintenance(request)).toEqual({
		revision: 4,
		operation_id: "op",
		active: true,
	});
	expect(post).toHaveBeenCalledWith(
		"/ops/spark/maintenance",
		request,
		expect.objectContaining({ retryCount: 0, redirect: "error" }),
	);
	post.mockResolvedValue({
		data: { code: 0, data: { revision: 4, operation_id: "other", active: true } },
	});
	await expect(sparkApi.changeMaintenance(request)).rejects.toThrow("结果尚未确认");
});

function modelStatus() {
	return {
		...status(),
		maintenance: "paused",
		maintenance_control: {
			revision: 3,
			operation_id: "maint",
			active: true,
			confirmed: true,
			can_pause: false,
			can_resume: true,
		},
		workers: [
			{
				worker_id: 9,
				state: "observed",
				maintenance: "paused",
				observed_at: Math.floor(Date.now() / 1000),
				running_count: 0,
				waiting_count: 0,
			},
		],
		model_control: {
			protocol_version: 1,
			host_id: "host",
			boot_id: "boot",
			observed_at: Math.floor(Date.now() / 1000),
			model_id: "qwen",
			label: "Local Qwen",
			state: "stopped",
			revision: 2,
			operation_id: "initial",
			action: "none",
			error_code: "",
			busy: false,
			can_start: true,
			can_stop: false,
		},
		can_start_model: true,
	};
}
it("accepts fixed model authority only with fresh matching identity and idle maintenance evidence", async () => {
	const base = modelStatus();
	get.mockResolvedValue({ data: { code: 0, data: base } });
	expect(await sparkApi.status()).toEqual(base);
	for (const mutate of [
		(s: ReturnType<typeof modelStatus>) => {
			s.model_control.boot_id = "old";
		},
		(s: ReturnType<typeof modelStatus>) => {
			s.model_control.host_id = "other";
		},
		(s: ReturnType<typeof modelStatus>) => {
			s.model_control.observed_at -= 31;
		},
		(s: ReturnType<typeof modelStatus>) => {
			s.model_control.revision = 0;
		},
		(s: ReturnType<typeof modelStatus>) => {
			s.model_control.state = "unknown";
		},
		(s: ReturnType<typeof modelStatus>) => {
			s.model_control.busy = true;
		},
		(s: ReturnType<typeof modelStatus>) => {
			s.model_control.can_start = false;
		},
		(s: ReturnType<typeof modelStatus>) => {
			s.maintenance_control.confirmed = false;
		},
		(s: ReturnType<typeof modelStatus>) => {
			if (s.workers[0]) s.workers[0].state = "busy";
		},
		(s: ReturnType<typeof modelStatus>) => {
			if (s.workers[0]) s.workers[0].running_count = 1;
		},
	]) {
		const data = modelStatus();
		mutate(data);
		get.mockResolvedValue({ data: { code: 0, data } });
		await expect(sparkApi.status()).rejects.toThrow("无法验证");
	}
});
it("accepts a busy operation only when maintenance resume and model actions are disabled", async () => {
	const data = modelStatus();
	Object.assign(data.model_control, {
		state: "starting",
		action: "start",
		busy: true,
		can_start: false,
	});
	data.can_start_model = false;
	data.maintenance_control.can_resume = false;
	get.mockResolvedValue({ data: { code: 0, data } });
	expect(await sparkApi.status()).toEqual(data);
	data.maintenance_control.can_resume = true;
	await expect(sparkApi.status()).rejects.toThrow("无法验证");
});
it("posts one fixed-target model CAS request and rejects mismatched or malformed receipts", async () => {
	const request = {
		boot_id: "boot",
		expected_revision: 2,
		maintenance_revision: 3,
		operation_id: "op",
		action: "start" as const,
	};
	const receipt = {
		...modelStatus().model_control,
		state: "starting",
		revision: 3,
		operation_id: "op",
		action: "start",
		busy: true,
		can_start: false,
	};
	post.mockResolvedValue({ data: { code: 0, data: receipt } });
	expect(await sparkApi.changeModel(request)).toEqual(receipt);
	expect(post).toHaveBeenCalledExactlyOnceWith(
		"/ops/spark/model",
		request,
		expect.objectContaining({ retryCount: 0, redirect: "error" }),
	);
	for (const patch of [
		{ boot_id: "other" },
		{ revision: 2 },
		{ operation_id: "other" },
		{ action: "stop" },
		{ state: "madeup" },
		{ busy: "yes" },
	]) {
		post.mockResolvedValue({ data: { code: 0, data: { ...receipt, ...patch } } });
		await expect(sparkApi.changeModel(request)).rejects.toThrow("结果尚未确认");
	}
});

it("rejects non-string model enum values and unsupported authority", async () => {
	for (const patch of [
		{ state: ["stopped"] },
		{ action: ["none"] },
		{ can_stop: "false" },
		{ state: "error", can_start: true },
	]) {
		const data = modelStatus();
		get.mockResolvedValue({
			data: { code: 0, data: { ...data, model_control: { ...data.model_control, ...patch } } },
		});
		await expect(sparkApi.status()).rejects.toThrow("无法验证");
	}
});

it("keeps unknown model observations visible when the server conservatively blocks maintenance resume", async () => {
	const base = modelStatus();
	const data = {
		...base,
		can_start_model: false,
		maintenance_control: { ...base.maintenance_control, can_resume: false },
		model_control: {
			...base.model_control,
			state: "unknown",
			can_start: false,
			error_code: "gate_unavailable",
		},
	};
	get.mockResolvedValue({ data: { code: 0, data } });
	expect(await sparkApi.status()).toEqual(data);
	get.mockResolvedValue({ data: { code: 0, data: { ...data, model_control: undefined } } });
	expect((await sparkApi.status()).maintenance_control?.can_resume).toBe(false);
});
