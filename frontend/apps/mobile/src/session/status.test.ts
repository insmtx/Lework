import { describe, expect, it } from "vitest";

import { rowSweepKind, statusGlyph, statusLabel, statusTone } from "./status";

describe("statusLabel", () => {
	it("把执行状态翻译成一行文案", () => {
		expect(statusLabel("running")).toBe("正在执行…");
		expect(statusLabel("waiting_permission")).toBe("等待你的处理");
		expect(statusLabel("idle")).toBe("执行完成");
		expect(statusLabel("error")).toBe("执行出错");
		expect(statusLabel("offline")).toBe("节点离线");
	});

	it("null 返回空串，状态条整行不渲染", () => {
		expect(statusLabel(null)).toBe("");
	});
});

describe("statusTone", () => {
	it("区分信息、警告、错误、成功与弱化", () => {
		expect(statusTone("running")).toBe("info");
		expect(statusTone("waiting_permission")).toBe("warn");
		expect(statusTone("error")).toBe("error");
		expect(statusTone("idle")).toBe("success");
		expect(statusTone("offline")).toBe("muted");
	});

	it("null 用弱化色", () => {
		expect(statusTone(null)).toBe("muted");
	});
});

describe("statusGlyph", () => {
	it("每个状态有自己的单符号，running 交给转圈动画", () => {
		expect(statusGlyph("running")).toBe("");
		expect(statusGlyph("waiting_permission")).toBe("!");
		expect(statusGlyph("idle")).toBe("✓");
		expect(statusGlyph("error")).toBe("✕");
		expect(statusGlyph("offline")).toBe("⊘");
	});

	it("null 不产生符号", () => {
		expect(statusGlyph(null)).toBe("");
	});
});

describe("rowSweepKind", () => {
	it("运行与等待铺整行扫光，出错用静态底色，其余不铺", () => {
		expect(rowSweepKind("running")).toBe("sweep");
		expect(rowSweepKind("waiting_permission")).toBe("sweep");
		expect(rowSweepKind("error")).toBe("static");
		expect(rowSweepKind("idle")).toBeNull();
		expect(rowSweepKind("offline")).toBeNull();
		expect(rowSweepKind(null)).toBeNull();
	});
});
