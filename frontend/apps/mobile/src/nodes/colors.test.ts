import { describe, expect, it } from "vitest";

import { NODE_COLOR_OPTIONS, nodeColorSwatch } from "./colors";

describe("nodeColorSwatch", () => {
	it("覆盖 8 个预设且每个都有可显示色值", () => {
		expect(NODE_COLOR_OPTIONS).toHaveLength(8);
		for (const option of NODE_COLOR_OPTIONS) {
			expect(option.label).not.toBe("");
			expect(option.swatch).toMatch(/^#[0-9a-f]{6}$/i);
			expect(nodeColorSwatch(option.token)).toBe(option.swatch);
		}
	});

	it("未设色与未知 token 返回 null", () => {
		expect(nodeColorSwatch(undefined)).toBeNull();
		expect(nodeColorSwatch(null)).toBeNull();
		expect(nodeColorSwatch("")).toBeNull();
		expect(nodeColorSwatch("chartreuse")).toBeNull();
	});

	it("原型链上的名字不能被当成颜色", () => {
		for (const token of ["constructor", "toString", "valueOf", "hasOwnProperty"]) {
			expect(nodeColorSwatch(token)).toBeNull();
		}
	});
});
