import { beforeEach, expect, it, vi } from "vitest";

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("./client", () => ({ apiClient: { get, post } }));

import { sparkWorkflowApi } from "./sparkWorkflowApi";

const op = () => ({
	operation_id: "op",
	action: "start",
	kind: "workflow",
	phase: "accepted",
	version: 1,
	created_at: 100,
	updated_at: 100,
	deadline: 1300,
	error_code: "",
	needs_attention: false,
	maintenance_retained: true,
});
const request = {
	operation_id: "op",
	action: "start" as const,
	boot_id: "boot",
	expected_model_revision: 1,
	expected_maintenance_revision: 1,
};
beforeEach(() => vi.clearAllMocks());
it("sends one fixed begin and validates exact operation identity", async () => {
	post.mockResolvedValue({ data: { code: 0, data: op() } });
	expect(await sparkWorkflowApi.begin(request)).toEqual(op());
	expect(post).toHaveBeenCalledWith(
		"/ops/spark/workflows",
		request,
		expect.objectContaining({ retryCount: 0, redirect: "error" }),
	);
	post.mockResolvedValue({
		data: { code: 0, data: { ...op(), operation_id: "other" } },
	});
	await expect(sparkWorkflowApi.begin(request)).rejects.toThrow();
	expect(post).toHaveBeenCalledTimes(2);
});
it("rejects invented phases errors missing booleans and unsafe counters", async () => {
	for (const change of [
		{ phase: "done" },
		{ error_code: "secret raw remote text" },
		{ needs_attention: undefined },
		{ version: 0 },
		{ deadline: 1 },
		{ kind: "shell" },
	]) {
		get.mockResolvedValue({ data: { code: 0, data: { ...op(), ...change } } });
		await expect(sparkWorkflowApi.get("op")).rejects.toThrow();
	}
});
it("does not label unavailable storage as unsupported and strips extra diagnostic fields", async () => {
	get.mockRejectedValue({ status: 503 });
	await expect(sparkWorkflowApi.current()).rejects.toThrow();
	get.mockResolvedValue({
		data: {
			code: 0,
			data: {
				enabled: true,
				current: { ...op(), secret: "hidden" },
				can_start: false,
				can_stop: false,
			},
		},
	});
	expect((await sparkWorkflowApi.current()).current).toEqual(op());
});
it("rejects path injection before a network request", async () => {
	await expect(sparkWorkflowApi.get("../model")).rejects.toThrow();
	expect(get).not.toHaveBeenCalled();
});
