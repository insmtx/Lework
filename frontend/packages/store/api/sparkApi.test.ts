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
	expect((await sparkApi.status()).workers[0].state).toBe("observed");
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
