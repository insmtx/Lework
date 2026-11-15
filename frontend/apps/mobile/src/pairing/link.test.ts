import { describe, expect, it } from "vitest";

import { buildPairPayload, normalizeCode, parsePairLink } from "./link";

describe("parsePairLink", () => {
	it("解析 lework://node/pair 链接", () => {
		const payload = parsePairLink("lework://node/pair?url=https%3A%2F%2Fbox%3A8787%2Fcp&code=ab234");
		expect(payload).toEqual({ baseUrl: "https://box:8787/cp", code: "AB234" });
	});

	it("解析 Expo Go 的 exp:// 链接", () => {
		const payload = parsePairLink(
			"exp://dev.ckeyer.com:8082/--/node/pair?url=http%3A%2F%2Fbox%3A8787&code=ZZ234",
		);
		expect(payload).toEqual({ baseUrl: "http://box:8787", code: "ZZ234" });
	});

	it("缺 code 或缺 url 都报错", () => {
		expect(() => parsePairLink("lework://node/pair?url=http%3A%2F%2Fbox%3A8787")).toThrow(/配对码/);
		expect(() => parsePairLink("lework://node/pair?code=AB234")).toThrow(/地址/);
		expect(() => parsePairLink("lework://node/pair?url=&code=AB234")).toThrow();
	});

	it("扫到不带参数的 Expo 二维码时给出专属提示", () => {
		expect(() => parsePairLink("exp://192.168.1.5:8082")).toThrow(/Expo/);
	});

	it("码长度不在 4..8 时报错", () => {
		expect(() => parsePairLink("lework://node/pair?url=http%3A%2F%2Fbox%3A8787&code=ABC")).toThrow(
			/配对码/,
		);
		expect(() =>
			parsePairLink("lework://node/pair?url=http%3A%2F%2Fbox%3A8787&code=AB2345678"),
		).toThrow(/配对码/);
	});

	it("码里的分隔符被丢弃后仍算合法", () => {
		// 手抄或扫码枪常带空格/连字符；它们不可能出现在合法短码里，直接丢掉。
		const payload = parsePairLink("lework://node/pair?url=http%3A%2F%2Fbox%3A8787&code=ab%20234");
		expect(payload.code).toBe("AB234");
	});
});

describe("buildPairPayload", () => {
	it("拒绝空 url 与空 code", () => {
		expect(() => buildPairPayload("", "AB234")).toThrow(/地址/);
		expect(() => buildPairPayload("http://box:8787", "  ")).toThrow(/配对码/);
	});
});

describe("normalizeCode", () => {
	it("去空白、转大写、丢弃分隔符", () => {
		expect(normalizeCode(" ab 234 ")).toBe("AB234");
		expect(normalizeCode("ab-234")).toBe("AB234");
	});
});
