import { ApiError } from "@lework/api";
import { describe, expect, it } from "vitest";

import {
	claimExpiryLabel,
	describeClaimError,
	isCompleteCode,
	normalizeCodeInput,
} from "./claim";

describe("normalizeCodeInput", () => {
	it("去空白、转大写、丢弃非字母数字", () => {
		expect(normalizeCodeInput(" ab 234 ")).toBe("AB234");
		expect(normalizeCodeInput("ab-234")).toBe("AB234");
		expect(normalizeCodeInput("")).toBe("");
	});
});

describe("isCompleteCode", () => {
	it("只在 4..8 位时为真", () => {
		expect(isCompleteCode("ABC")).toBe(false);
		expect(isCompleteCode("AB23")).toBe(true);
		expect(isCompleteCode("AB234567")).toBe(true);
		expect(isCompleteCode("AB2345678")).toBe(false);
	});
});

describe("claimExpiryLabel", () => {
	it("显示剩余时间并在过期后给固定文案", () => {
		const now = Date.parse("2026-11-16T10:00:00Z");
		expect(claimExpiryLabel("2026-11-16T10:04:30Z", now)).toBe("4 分 30 秒后失效");
		expect(claimExpiryLabel("2026-11-16T09:59:00Z", now)).toBe("已失效，请让那台机器重新出码");
	});
});

describe("describeClaimError", () => {
	it("覆盖 404/429/401/网络", () => {
		expect(describeClaimError(new ApiError(404, "claim_not_found", ""))).toBe(
			"码无效或已过期，请让那台机器重新出码",
		);
		expect(describeClaimError(new ApiError(429, "too_many_attempts", ""))).toBe(
			"尝试次数过多，请稍后再试",
		);
		expect(describeClaimError(new ApiError(401, "unauthorized", ""))).toBe("登录已过期，请重新登录");
		expect(describeClaimError(new TypeError("Network request failed"))).toBe(
			"网络不通，请检查服务器地址",
		);
	});
});
