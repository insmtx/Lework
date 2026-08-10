import { describe, expect, it } from "vitest";

import {
	buildComposerFolderUploadSummaryMessage,
	COMPOSER_UPLOAD_ACCEPT,
	getComposerUploadAccept,
	getNativeFileInputAccept,
	getUploadFileExtension,
	isComposerUploadAllowedFileName,
	partitionComposerFolderFiles,
} from "./composer-upload";

describe("composer-upload", () => {
	it("disables the native extension filter on Linux", () => {
		expect(getComposerUploadAccept("Linux x86_64")).toBeUndefined();
		expect(getComposerUploadAccept("linux")).toBeUndefined();
		expect(getComposerUploadAccept("MacIntel")).toBe(COMPOSER_UPLOAD_ACCEPT);
		expect(getComposerUploadAccept("Win32")).toBe(COMPOSER_UPLOAD_ACCEPT);
		expect(getNativeFileInputAccept("image/*", "Linux x86_64")).toBeUndefined();
		expect(getNativeFileInputAccept(".zip,.md", "linux")).toBeUndefined();
		expect(getNativeFileInputAccept("image/*", "MacIntel")).toBe("image/*");
	});

	it("exposes images in the native file picker", () => {
		expect(COMPOSER_UPLOAD_ACCEPT.split(",")).toEqual(expect.arrayContaining(["image/*"]));
		expect(COMPOSER_UPLOAD_ACCEPT).not.toContain("video/*");
	});

	it("accepts allowed extensions", () => {
		for (const fileName of [
			"report.pdf",
			"notes.docx",
			"sheet.XLSX",
			"slide.ppt",
			"readme.md",
			"page.html",
			"legacy.htm",
			"photo.JPG",
			"photo.jpeg",
			"photo.png",
			"animation.gif",
			"bitmap.bmp",
			"photo.webp",
			"vector.svg",
			"plain.txt",
		]) {
			expect(isComposerUploadAllowedFileName(fileName), fileName).toBe(true);
		}
	});

	it("rejects unsupported extensions", () => {
		expect(isComposerUploadAllowedFileName("package.zip")).toBe(false);
		expect(isComposerUploadAllowedFileName("py.typed")).toBe(false);
		expect(isComposerUploadAllowedFileName("no-extension")).toBe(false);
		expect(isComposerUploadAllowedFileName("movie.mp4")).toBe(false);
		expect(isComposerUploadAllowedFileName("movie.mov")).toBe(false);
		expect(isComposerUploadAllowedFileName("movie.avi")).toBe(false);
	});

	it("extracts extension from nested paths", () => {
		expect(getUploadFileExtension("folder/sub/file.pdf")).toBe(".pdf");
	});

	it("partitions folder files by empty and type", () => {
		const pdf = new File(["content"], "a.pdf", { type: "application/pdf" });
		const empty = new File([], "empty.txt", { type: "text/plain" });
		const zip = new File(["zip"], "archive.zip", { type: "application/zip" });

		const result = partitionComposerFolderFiles([pdf, empty, zip]);
		expect(result.uploadable).toHaveLength(1);
		expect(result.skippedEmpty).toHaveLength(1);
		expect(result.skippedType).toHaveLength(1);
	});

	it("builds folder upload summary message", () => {
		expect(buildComposerFolderUploadSummaryMessage(2, 1, 1)).toBe(
			"文件夹上传成功，已跳过 1 个空文件，已跳过 1 个不支持的文件类型",
		);
		expect(buildComposerFolderUploadSummaryMessage(0, 2, 0)).toBe("已跳过 2 个空文件");
	});
});
