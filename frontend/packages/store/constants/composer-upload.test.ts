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

	it("accepts allowed extensions", () => {
		expect(isComposerUploadAllowedFileName("report.pdf")).toBe(true);
		expect(isComposerUploadAllowedFileName("notes.docx")).toBe(true);
		expect(isComposerUploadAllowedFileName("sheet.XLSX")).toBe(true);
		expect(isComposerUploadAllowedFileName("slide.ppt")).toBe(true);
		expect(isComposerUploadAllowedFileName("readme.md")).toBe(true);
		expect(isComposerUploadAllowedFileName("photo.JPG")).toBe(true);
		expect(isComposerUploadAllowedFileName("plain.txt")).toBe(true);
	});

	it("rejects unsupported extensions", () => {
		expect(isComposerUploadAllowedFileName("package.zip")).toBe(false);
		expect(isComposerUploadAllowedFileName("py.typed")).toBe(false);
		expect(isComposerUploadAllowedFileName("no-extension")).toBe(false);
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
