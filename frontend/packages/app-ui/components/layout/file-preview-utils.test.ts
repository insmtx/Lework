import { describe, expect, it } from "vitest";

import { detectFilePreviewKind } from "./file-preview-utils";

describe("detectFilePreviewKind", () => {
	it.each([
		"jpg",
		"jpeg",
		"png",
		"gif",
		"bmp",
		"webp",
		"svg",
	])("previews .%s files as images when MIME type is unavailable", (extension) => {
		expect(detectFilePreviewKind({ name: `image.${extension}` })).toBe("image");
	});

	it.each([
		"mp4",
		"mov",
		"avi",
	])("previews .%s files as videos when MIME type is unavailable", (extension) => {
		expect(detectFilePreviewKind({ name: `video.${extension}` })).toBe("video");
	});

	it("recognizes image and video MIME types", () => {
		expect(detectFilePreviewKind({ name: "asset", mimeType: "image/avif" })).toBe("image");
		expect(detectFilePreviewKind({ name: "asset", mimeType: "video/webm" })).toBe("video");
	});
});
