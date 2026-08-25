// @vitest-environment jsdom

import {
	normalizeAPIBaseURL,
	PRIVATE_DEPLOYMENT_MODE_STORAGE_KEY,
	readServerBaseURL,
	resolveIsPrivateDeployment,
	SERVER_CONFIG_STORAGE_KEY,
	saveServerBaseURL,
	testServerConnection,
} from "@leros/store";
import { afterEach, describe, expect, it, vi } from "vitest";

const LEGACY_PRIVATE_SERVER_CONFIG_STORAGE_KEY = "leros-private-server-base-url";

describe("private server config", () => {
	afterEach(() => {
		window.localStorage.removeItem(SERVER_CONFIG_STORAGE_KEY);
		window.localStorage.removeItem(LEGACY_PRIVATE_SERVER_CONFIG_STORAGE_KEY);
		window.localStorage.removeItem(PRIVATE_DEPLOYMENT_MODE_STORAGE_KEY);
		vi.unstubAllGlobals();
	});

	it("forces private deployment when the localStorage override key exists", () => {
		expect(resolveIsPrivateDeployment()).toBe(false);

		window.localStorage.setItem(PRIVATE_DEPLOYMENT_MODE_STORAGE_KEY, "1");
		expect(resolveIsPrivateDeployment()).toBe(true);
	});

	it("normalizes root and v1 service addresses", () => {
		expect(normalizeAPIBaseURL(" https://leros.example.com/ ")).toBe(
			"https://leros.example.com/v1",
		);
		expect(normalizeAPIBaseURL("http://localhost:8080/v1/")).toBe("http://localhost:8080/v1");
		expect(normalizeAPIBaseURL("https://host.example.com/api")).toBe(
			"https://host.example.com/api/v1",
		);
	});

	it("rejects unsafe or incomplete service addresses", () => {
		expect(() => normalizeAPIBaseURL("leros.example.com")).toThrow("完整的");
		expect(() => normalizeAPIBaseURL("file:///tmp/leros")).toThrow("仅支持");
		expect(() => normalizeAPIBaseURL("https://user:pass@example.com")).toThrow("用户名或密码");
		expect(() => normalizeAPIBaseURL("https://example.com?token=secret")).toThrow("查询参数");
	});

	it("persists the normalized service address", () => {
		saveServerBaseURL("https://leros.example.com");

		expect(window.localStorage.getItem(SERVER_CONFIG_STORAGE_KEY)).toBe(
			"https://leros.example.com/v1",
		);
		expect(readServerBaseURL()).toBe("https://leros.example.com/v1");
	});

	it("migrates the legacy private server address into the shared key", () => {
		window.localStorage.setItem(
			LEGACY_PRIVATE_SERVER_CONFIG_STORAGE_KEY,
			"https://legacy.example.com/v1",
		);

		expect(readServerBaseURL()).toBe("https://legacy.example.com/v1");
		expect(window.localStorage.getItem(SERVER_CONFIG_STORAGE_KEY)).toBe(
			"https://legacy.example.com/v1",
		);
		expect(window.localStorage.getItem(LEGACY_PRIVATE_SERVER_CONFIG_STORAGE_KEY)).toBeNull();
	});

	it("accepts only a Lework GlobalConfig response", async () => {
		const fetchMock = vi.fn().mockResolvedValue(
			new Response(JSON.stringify({ data: { edition: "enterprise" } }), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		vi.stubGlobal("fetch", fetchMock);

		await expect(testServerConnection("https://leros.example.com")).resolves.toBe(
			"https://leros.example.com/v1",
		);
		expect(fetchMock).toHaveBeenCalledWith(
			"https://leros.example.com/v1/GlobalConfig",
			expect.objectContaining({ method: "GET" }),
		);

		fetchMock.mockResolvedValueOnce(
			new Response(JSON.stringify({ data: { name: "other-service" } }), {
				status: 200,
				headers: { "Content-Type": "application/json" },
			}),
		);
		await expect(testServerConnection("https://other.example.com")).rejects.toThrow(
			"响应格式不正确",
		);
	});
});
