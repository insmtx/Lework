import { afterEach, describe, expect, it, vi } from "vitest";

import { pollRuntimeStatus } from "./historyLoader";

vi.mock("../api/sessionApi", () => ({
	sessionApi: {
		get: vi.fn(),
	},
}));

import { sessionApi } from "../api/sessionApi";

describe("pollRuntimeStatus", () => {
	afterEach(() => {
		vi.useRealTimers();
		vi.clearAllMocks();
	});

	it("shouldAbort 为真时立即停止且不再请求 GetSession", async () => {
		vi.useFakeTimers();
		const get = vi.mocked(sessionApi.get);
		const promise = pollRuntimeStatus("session-1", 60_000, 1, () => true);
		await vi.advanceTimersByTimeAsync(0);
		await expect(promise).resolves.toBe("aborted");
		expect(get).not.toHaveBeenCalled();
	});

	it("responding 时返回 responding，不把已 responding 当成继续空转的条件", async () => {
		vi.useFakeTimers();
		const get = vi.mocked(sessionApi.get).mockResolvedValue({
			data: { data: { runtime_status: "responding", message_count: 2 } },
		} as never);
		const promise = pollRuntimeStatus("session-1", 60_000, 1);
		await vi.advanceTimersByTimeAsync(2000);
		await expect(promise).resolves.toEqual({ status: "responding", messageCount: 2 });
		expect(get).toHaveBeenCalledTimes(1);
	});

	it("轮询中途 shouldAbort 变真时返回 aborted", async () => {
		vi.useFakeTimers();
		let aborted = false;
		vi.mocked(sessionApi.get).mockImplementation(async () => {
			aborted = true;
			return {
				data: { data: { runtime_status: "idle", message_count: 1 } },
			} as never;
		});
		const promise = pollRuntimeStatus("session-1", 60_000, 1, () => aborted);
		await vi.advanceTimersByTimeAsync(2000);
		await expect(promise).resolves.toBe("aborted");
	});
});
