import { describe, expect, it, vi } from "vitest";

import { connectToControlPlane } from "./connect";

const payload = { baseUrl: "http://dev.ckeyer.com", token: "abc-123" };

describe("connectToControlPlane", () => {
	it("先验证再保存", async () => {
		const calls: string[] = [];
		const settings = await connectToControlPlane(payload, {
			verify: async () => {
				calls.push("verify");
			},
			save: async () => {
				calls.push("save");
			},
			notifications: true,
		});

		expect(calls).toEqual(["verify", "save"]);
		expect(settings).toEqual({
			baseUrl: "http://dev.ckeyer.com",
			token: "abc-123",
			notifications: true,
		});
	});

	it("验证失败时不保存旧配置", async () => {
		const save = vi.fn();
		await expect(
			connectToControlPlane(payload, {
				verify: async () => {
					throw new Error("Network request failed");
				},
				save,
				notifications: true,
			}),
		).rejects.toThrow("Network request failed");
		expect(save).not.toHaveBeenCalled();
	});

	it("沿用用户原有的推送偏好", async () => {
		const settings = await connectToControlPlane(payload, {
			verify: async () => undefined,
			save: async () => undefined,
			notifications: false,
		});
		expect(settings.notifications).toBe(false);
	});
});
