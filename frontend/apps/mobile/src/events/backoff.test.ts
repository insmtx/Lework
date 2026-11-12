import { describe, expect, it } from "vitest";
import { nextRetryDelay } from "./backoff";

describe("nextRetryDelay", () => {
	it("从 1 秒开始指数退避", () => {
		expect(nextRetryDelay(0)).toBe(1000);
		expect(nextRetryDelay(3)).toBe(8000);
	});

	it("封顶 30 秒", () => {
		expect(nextRetryDelay(99)).toBe(30000);
	});
});
