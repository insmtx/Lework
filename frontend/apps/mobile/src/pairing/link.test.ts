import { describe, expect, it } from "vitest";

import { buildPairPayload, parsePairLink } from "./link";

describe("parsePairLink", () => {
	it("解析 lework:// 链接", () => {
		const payload = parsePairLink("lework://connect?url=http%3A%2F%2Fdev.ckeyer.com&token=abc-123");
		expect(payload).toEqual({ baseUrl: "http://dev.ckeyer.com", token: "abc-123" });
	});

	it("解析 Expo Go 的 exp:// 链接", () => {
		const payload = parsePairLink(
			"exp://dev.ckeyer.com:8082/--/connect?url=https%3A%2F%2Fcp.example.com&token=t1",
		);
		expect(payload).toEqual({ baseUrl: "https://cp.example.com", token: "t1" });
	});

	it("解码带空格的令牌", () => {
		const payload = parsePairLink("lework://connect?url=http%3A%2F%2Fbox%3A8787&token=a%20b%2Fc");
		expect(payload.token).toBe("a b/c");
		expect(payload.baseUrl).toBe("http://box:8787");
	});

	it("忽略末尾斜杠与多余空白", () => {
		const payload = parsePairLink("  lework://connect?url=http%3A%2F%2Fbox%3A8787%2F&token=t  ");
		expect(payload.baseUrl).toBe("http://box:8787");
	});

	it("缺参数、缺令牌、缺地址都报错", () => {
		expect(() => parsePairLink("lework://connect")).toThrow();
		expect(() => parsePairLink("lework://connect?url=http%3A%2F%2Fbox%3A8787")).toThrow(/令牌/);
		expect(() => parsePairLink("lework://connect?url=&token=t")).toThrow();
	});

	it("同名参数取第一次出现", () => {
		const payload = parsePairLink(
			"lework://connect?url=http%3A%2F%2Fa.example&url=http%3A%2F%2Fb.example&token=t",
		);
		expect(payload.baseUrl).toBe("http://a.example");
	});
});

describe("buildPairPayload", () => {
	it("拒绝空 url 与空 token", () => {
		expect(() => buildPairPayload("", "t")).toThrow();
		expect(() => buildPairPayload("http://box:8787", "  ")).toThrow(/令牌/);
	});
});
