import { describe, expect, it } from "vitest";

import { formatFileSize, mimeTypeFor, previewKind, truncationNotice } from "./viewer";

describe("previewKind", () => {
	it("按扩展名分流", () => {
		expect(previewKind("plan.md")).toBe("markdown");
		expect(previewKind("plan.MDX")).toBe("markdown");
		expect(previewKind("main.go")).toBe("auto");
		expect(previewKind("photo.PNG")).toBe("image");
		expect(previewKind("report.pdf")).toBe("binary");
		expect(previewKind("budget.xlsx")).toBe("binary");
		expect(previewKind("release.zip")).toBe("binary");
		expect(previewKind("Dockerfile")).toBe("auto");
	});
});

describe("mimeTypeFor", () => {
	it("覆盖常见类型，未知返回 undefined", () => {
		expect(mimeTypeFor("report.pdf")).toBe("application/pdf");
		expect(mimeTypeFor("plan.md")).toBe("text/markdown");
		expect(mimeTypeFor("a.unknown")).toBeUndefined();
	});
});

describe("truncationNotice", () => {
	it("只在被截断时提示", () => {
		expect(truncationNotice(true)).toBe("文件较大，仅显示前 512 KiB");
		expect(truncationNotice(false)).toBeNull();
	});
});

describe("formatFileSize", () => {
	it("按 B / KB / MB 显示", () => {
		expect(formatFileSize(512)).toBe("512 B");
		expect(formatFileSize(2048)).toBe("2.0 KB");
		expect(formatFileSize(3 * 1024 * 1024)).toBe("3.0 MB");
	});
});
