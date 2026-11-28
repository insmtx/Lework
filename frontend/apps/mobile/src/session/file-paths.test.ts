import { describe, expect, it } from "vitest";

import { findFilePathSpans } from "./file-paths";

// 断言只关心片段文本时用这个，避免每个用例重复 start/end。
function paths(content: string): string[] {
	return findFilePathSpans(content).map((span) => span.path);
}

describe("findFilePathSpans", () => {
	it("中文冒号紧贴路径时只取路径本身", () => {
		expect(paths("设计文档写好了：docs/superpowers/specs/2026-11-28-a-design.md")).toEqual([
			"docs/superpowers/specs/2026-11-28-a-design.md",
		]);
	});

	it("同一段文本里的多个路径都识别", () => {
		expect(paths("见 docs/a.md 和 src/b.go")).toEqual(["docs/a.md", "src/b.go"]);
	});

	it("中文标点、括号和句号不属于路径", () => {
		expect(paths("（docs/a.md），另见 ./b.md。")).toEqual(["docs/a.md", "./b.md"]);
		expect(paths("见 docs/a.md.")).toEqual(["docs/a.md"]);
	});

	it("绝对路径、Windows 盘符与 UNC 路径", () => {
		expect(paths("/repo/docs/a.md")).toEqual(["/repo/docs/a.md"]);
		expect(paths("../docs/a.md")).toEqual(["../docs/a.md"]);
		expect(paths("~/docs/a.md")).toEqual(["~/docs/a.md"]);
		expect(paths("C:\\repo\\docs\\a.md")).toEqual(["C:\\repo\\docs\\a.md"]);
		expect(paths("\\\\box\\share\\a.md")).toEqual(["\\\\box\\share\\a.md"]);
	});

	it("百分号编码保留在路径里", () => {
		expect(paths("docs/My%20Plan.md")).toEqual(["docs/My%20Plan.md"]);
	});

	it("外链、协议相对 URL 与域名开头的 token 不识别", () => {
		expect(paths("https://example.com/a.md")).toEqual([]);
		expect(paths("//cdn.example.com/a.png")).toEqual([]);
		expect(paths("example.com/a.md")).toEqual([]);
		expect(paths("mailto:a@b.c")).toEqual([]);
	});

	it("不是文件的斜杠文本不识别", () => {
		expect(paths("见 and/or 与 24/7")).toEqual([]);
	});

	it("无斜杠文件名、无扩展名、首段带点的相对路径不识别", () => {
		expect(paths("README.md")).toEqual([]);
		expect(paths("config.yaml")).toEqual([]);
		expect(paths("docs/README")).toEqual([]);
		expect(paths("1.2.3/README.md")).toEqual([]);
	});

	it("start/end 是原串下标，path 等于切片", () => {
		const content = "看 docs/a.md 吧";
		const spans = findFilePathSpans(content);
		expect(spans).toHaveLength(1);
		expect(content.slice(spans[0].start, spans[0].end)).toBe(spans[0].path);
		expect(spans[0].start).toBe(2);
	});
});
