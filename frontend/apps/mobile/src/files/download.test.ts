import { describe, expect, it } from "vitest";

import type { Settings } from "../settings/settings";
import { fileDownloadUrl, previewCachePath } from "./download";

const settings: Settings = {
	baseUrl: "https://cp.example",
	email: "a@b.c",
	sessionToken: "token",
	notifications: false,
};

describe("fileDownloadUrl", () => {
	it("拼接节点与编码后的 query", () => {
		expect(fileDownloadUrl(settings, "node 1", "/repo/docs", "a b.md")).toBe(
			"https://cp.example/api/nodes/node%201/files/download?directory=%2Frepo%2Fdocs&path=a%20b.md",
		);
	});
});

describe("previewCachePath", () => {
	it("落在缓存目录并保留原文件名", () => {
		const target = previewCachePath("/cache/", "n1", "docs/report.pdf");
		expect(target.startsWith("/cache/lework-preview-")).toBe(true);
		expect(target.endsWith("report.pdf")).toBe(true);
	});

	it("同名文件用路径指纹区分", () => {
		expect(previewCachePath("/cache/", "n1", "a/report.pdf")).not.toBe(
			previewCachePath("/cache/", "n1", "b/report.pdf"),
		);
	});
});
