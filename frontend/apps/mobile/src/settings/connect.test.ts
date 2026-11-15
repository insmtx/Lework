import { describe, expect, it, vi } from "vitest";

import { buildConnectionSettings, connectionChanged, connectToControlPlane } from "./connect";
import type { Settings } from "./settings";

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

describe("buildConnectionSettings", () => {
	it("去除地址首尾空白与末尾斜杠，并保留通知开关", () => {
		expect(
			buildConnectionSettings({
				baseUrl: "  https://box:8787///  ",
				token: "  secret  ",
				notifications: true,
				deviceID: "dev_1",
			}),
		).toEqual({
			baseUrl: "https://box:8787",
			token: "secret",
			notifications: true,
			deviceID: "dev_1",
		});
	});

	it("令牌为空时拒绝保存", () => {
		expect(() =>
			buildConnectionSettings({
				baseUrl: "https://box:8787",
				token: "   ",
				notifications: false,
			}),
		).toThrow("token");
	});

	it("地址非法时沿用 normalizeBaseUrl 的错误", () => {
		expect(() =>
			buildConnectionSettings({ baseUrl: "box:8787", token: "t", notifications: false }),
		).toThrow("http");
	});
});

describe("connectionChanged", () => {
	const base: Settings = { baseUrl: "https://a", token: "t", notifications: false };

	it("地址或令牌变化时返回 true", () => {
		expect(connectionChanged(base, { ...base, baseUrl: "https://b" })).toBe(true);
		expect(connectionChanged(base, { ...base, token: "other" })).toBe(true);
	});

	it("仅通知开关变化不算换服务器", () => {
		expect(connectionChanged(base, { ...base, notifications: true })).toBe(false);
	});

	it("之前没有设置时视为变化", () => {
		expect(connectionChanged(null, base)).toBe(true);
	});
});
