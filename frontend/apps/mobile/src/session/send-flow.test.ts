import { describe, expect, it, vi } from "vitest";
import { runSend } from "./send-flow";

describe("runSend", () => {
	it("发送成功后才清空草稿", async () => {
		const send = vi.fn().mockResolvedValue(undefined);
		const clear = vi.fn();
		await runSend(send, clear);
		expect(send).toHaveBeenCalledTimes(1);
		expect(clear).toHaveBeenCalledTimes(1);
	});

	it("发送失败时保留草稿", async () => {
		const send = vi.fn().mockRejectedValue(new Error("prompt failed"));
		const clear = vi.fn();
		await expect(runSend(send, clear)).rejects.toThrow("prompt failed");
		expect(clear).not.toHaveBeenCalled();
	});
});
