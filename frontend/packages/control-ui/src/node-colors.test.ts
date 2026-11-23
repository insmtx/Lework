import { describe, expect, it } from "vitest";
import { NODE_COLOR_STYLES, NODE_COLOR_TOKENS, nodeColorStyle } from "./node-colors";

describe("nodeColorStyle", () => {
	it("覆盖 8 个预设且每个字段都有实际类名", () => {
		expect(NODE_COLOR_TOKENS).toHaveLength(8);
		for (const token of NODE_COLOR_TOKENS) {
			const style = NODE_COLOR_STYLES[token];
			expect(style.label).not.toBe("");
			for (const value of [style.bar, style.line, style.wash]) {
				expect(value).toContain("bg-gradient-to-");
				expect(value).toContain("from-");
			}
			expect(style.dot).toContain("bg-gradient-to-br");
			expect(style.dot).toContain("ring-1");
		}
	});

	it("未知 token 与空值降级为 null", () => {
		expect(nodeColorStyle("chartreuse")).toBeNull();
		expect(nodeColorStyle("")).toBeNull();
		expect(nodeColorStyle(undefined)).toBeNull();
		expect(nodeColorStyle(null)).toBeNull();
	});

	it("原型链上的名字不能被当成颜色", () => {
		// 服务端只校验形状，"constructor" 能通过 regex；普通对象下标会命中原型，
		// 返回函数而不是 null，导致渲染出没有样式的空圆点。
		for (const token of ["constructor", "toString", "valueOf", "hasOwnProperty"]) {
			expect(nodeColorStyle(token)).toBeNull();
		}
	});
});
