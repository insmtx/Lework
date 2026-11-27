import { describe, expect, it } from "vitest";

import { fileRoute } from "../config";
import { fileBaseDirectory, resolveFileLink } from "./file-links";

describe("resolveFileLink", () => {
	it("相对路径按会话目录解析", () => {
		expect(resolveFileLink("docs/plan.md", "/repo")).toEqual({
			directory: "/repo",
			path: "docs/plan.md",
		});
	});

	it("去掉 fragment、query 并做百分号解码", () => {
		expect(resolveFileLink("./docs/My%20Plan.md?plain=1#L10", "/repo")).toEqual({
			directory: "/repo",
			path: "docs/My Plan.md",
		});
	});

	it("POSIX 绝对路径按文件所在目录请求", () => {
		expect(resolveFileLink("/repo/docs/plan.md", "/repo")).toEqual({
			directory: "/repo/docs/",
			path: "plan.md",
		});
	});

	it("file:// 等价于绝对路径", () => {
		expect(resolveFileLink("file:///repo/docs/plan.md", "/repo")).toEqual({
			directory: "/repo/docs/",
			path: "plan.md",
		});
	});

	it("Windows 绝对路径不被当成 URL scheme", () => {
		expect(resolveFileLink("C:\\repo\\docs\\plan.md", "/repo")).toEqual({
			directory: "C:\\repo\\docs\\",
			path: "plan.md",
		});
		expect(resolveFileLink("C:/repo/docs/plan.md", "/repo")).toEqual({
			directory: "C:/repo/docs/",
			path: "plan.md",
		});
	});

	it("外部链接与锚点不拦截", () => {
		expect(resolveFileLink("https://example.com/plan.md", "/repo")).toBeNull();
		expect(resolveFileLink("//cdn.example.com/a.png", "/repo")).toBeNull();
		expect(resolveFileLink("mailto:a@b.c", "/repo")).toBeNull();
		expect(resolveFileLink("#section", "/repo")).toBeNull();
	});

	it("相对路径没有会话目录时不拦截", () => {
		expect(resolveFileLink("docs/plan.md", "")).toBeNull();
	});

	it("目录链接不当作文件", () => {
		expect(resolveFileLink("/repo/docs/", "/repo")).toBeNull();
	});
});

describe("fileBaseDirectory", () => {
	it("子目录里的 markdown 继续按子目录解析相对链接", () => {
		expect(fileBaseDirectory("/repo", "docs/plan.md")).toBe("/repo/docs/");
		expect(fileBaseDirectory("/repo", "plan.md")).toBe("/repo");
		expect(fileBaseDirectory("/repo/docs/", "plan.md")).toBe("/repo/docs/");
	});

	it("Windows 分隔符跟随目录", () => {
		expect(fileBaseDirectory("C:\\repo", "docs\\plan.md")).toBe("C:\\repo\\docs\\");
	});
});

describe("fileRoute", () => {
	it("节点、目录和路径都编码进 query", () => {
		expect(fileRoute("node 1", "/repo/docs", "a b/plan.md")).toBe(
			"/file/node%201?directory=%2Frepo%2Fdocs&path=a%20b%2Fplan.md",
		);
	});
});
