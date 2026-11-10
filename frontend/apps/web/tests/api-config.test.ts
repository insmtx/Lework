import { normalizeAPIBaseURL } from "@lework/store/api/config";
import { describe, expect, it } from "vitest";

describe("normalizeAPIBaseURL", () => {
	it("accepts a same-origin relative base for the AIO gateway", () => {
		expect(normalizeAPIBaseURL("/v1")).toBe("/v1");
		expect(normalizeAPIBaseURL("/v1/")).toBe("/v1");
		expect(normalizeAPIBaseURL("/lework/v1")).toBe("/lework/v1");
	});

	it("keeps absolute URLs unchanged apart from a trailing slash", () => {
		expect(normalizeAPIBaseURL("http://192.168.1.10:8080")).toBe("http://192.168.1.10:8080/v1");
		expect(normalizeAPIBaseURL("https://lework.example.com/v1/")).toBe(
			"https://lework.example.com/v1",
		);
	});

	it("rejects protocol-relative and empty values", () => {
		expect(() => normalizeAPIBaseURL("//evil.example.com/v1")).toThrow();
		expect(() => normalizeAPIBaseURL("   ")).toThrow();
	});
});
