import { describe, expect, it } from "vitest";

import { bottomOffset, isNearBottom } from "./scroll";

describe("isNearBottom", () => {
	it("停在最底部时算贴底", () => {
		expect(isNearBottom({ offsetY: 600, contentHeight: 1000, viewportHeight: 400 })).toBe(true);
	});

	it("差一屏余量以内也算贴底", () => {
		expect(isNearBottom({ offsetY: 580, contentHeight: 1000, viewportHeight: 400 })).toBe(true);
	});

	it("向上翻得比较远时不贴底，流式更新不能把用户拽回去", () => {
		expect(isNearBottom({ offsetY: 400, contentHeight: 1000, viewportHeight: 400 })).toBe(false);
	});

	it("内容不足一屏时视为贴底", () => {
		expect(isNearBottom({ offsetY: 0, contentHeight: 120, viewportHeight: 400 })).toBe(true);
	});
});

describe("bottomOffset", () => {
	it("按真实内容高度贴底", () => {
		expect(bottomOffset(1200, 400)).toBe(800);
	});

	it("内容不足一屏时停在 0，由 flex-end 负责贴底", () => {
		expect(bottomOffset(300, 400)).toBe(0);
	});

	it("空内容不会产生负偏移", () => {
		expect(bottomOffset(0, 400)).toBe(0);
	});
});
