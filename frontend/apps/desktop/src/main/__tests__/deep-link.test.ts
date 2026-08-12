import { describe, expect, it } from "vitest";
import { extractDesktopServerURL } from "../deep-link";

describe("桌面端环境深链", () => {
	it("从启动参数中读取有效的 Lework 服务地址", () => {
		expect(
			extractDesktopServerURL([
				"electron",
				"app",
				"leros://open?server=https%3A%2F%2Fstaging.example.com%2Fv1",
			]),
		).toBe("https://staging.example.com/v1");
	});

	it("拒绝非 open 命令和不安全的服务地址", () => {
		expect(
			extractDesktopServerURL(["leros://other?server=https://staging.example.com"]),
		).toBeNull();
		expect(extractDesktopServerURL(["leros://open?server=file%3A%2F%2F%2Ftmp%2Fleros"])).toBeNull();
		expect(
			extractDesktopServerURL([
				"leros://open?server=https%3A%2F%2Fuser%3Apassword%40staging.example.com",
			]),
		).toBeNull();
	});
});
