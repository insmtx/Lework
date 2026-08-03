import { describe, expect, it } from "vitest";
import { isExternalHttpUrl, shouldOpenExternalUrl } from "../external-navigation";

describe("external-navigation", () => {
	it("识别 http(s) 外链", () => {
		expect(isExternalHttpUrl("https://example.com")).toBe(true);
		expect(isExternalHttpUrl("http://localhost:5173")).toBe(true);
		expect(isExternalHttpUrl("file:///index.html")).toBe(false);
		expect(isExternalHttpUrl("#/workbench")).toBe(false);
	});

	it("生产环境应拦截所有 http(s) 导航", () => {
		expect(shouldOpenExternalUrl("https://example.com")).toBe(true);
		expect(shouldOpenExternalUrl("file:///index.html")).toBe(false);
	});

	it("开发环境应允许 Vite 同源导航", () => {
		const devRendererUrl = "http://localhost:5173";

		expect(shouldOpenExternalUrl("http://localhost:5173/", devRendererUrl)).toBe(false);
		expect(shouldOpenExternalUrl("https://example.com", devRendererUrl)).toBe(true);
	});
});
