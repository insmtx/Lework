// @vitest-environment jsdom

import {
	normalizeAPIBaseURL,
	PRIVATE_SERVER_CONFIG_STORAGE_KEY,
	readPrivateServerBaseURL,
	savePrivateServerBaseURL,
	testServerConnection,
} from "@leros/store";
import { afterEach, describe, expect, it, vi } from "vitest";

describe("private server config", () => {
	afterEach(() => {
		window.localStorage.removeItem(PRIVATE_SERVER_CONFIG_STORAGE_KEY);
		vi.unstubAllGlobals();
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
		savePrivateServerBaseURL("https://leros.example.com");

		expect(window.localStorage.getItem(PRIVATE_SERVER_CONFIG_STORAGE_KEY)).toBe(
			"https://leros.example.com/v1",
		);
		expect(readPrivateServerBaseURL()).toBe("https://leros.example.com/v1");
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
