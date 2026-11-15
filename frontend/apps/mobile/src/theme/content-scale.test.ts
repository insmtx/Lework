import { describe, expect, it } from "vitest";

import { contentTextStyle } from "./content-scale";

describe("contentTextStyle", () => {
	it("标准倍率下保持基础字号", () => {
		expect(contentTextStyle(1, 12, 18)).toEqual({ fontSize: 12, lineHeight: 18 });
	});

	it("按倍率同时放大字号与行高", () => {
		expect(contentTextStyle(1.25, 12, 18)).toEqual({ fontSize: 15, lineHeight: 23 });
	});

	it("缩小倍率下不会得到 0 或负数字号", () => {
		expect(contentTextStyle(0.5, 12, 18)).toEqual({ fontSize: 6, lineHeight: 9 });
	});
});
