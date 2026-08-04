import { describe, expect, it } from "vitest";

import { getProjectFileIconSrc } from "./project-file-type-icon";

describe("getProjectFileIconSrc", () => {
	it.each(["gif", "bmp", "webp", "svg"])("uses the picture icon for .%s", (extension) => {
		const icon = getProjectFileIconSrc(`image.${extension}`);
		expect(icon).toBe(getProjectFileIconSrc("image.gif"));
		expect(icon).not.toBe(getProjectFileIconSrc("notes.txt"));
	});

	it.each(["mp4", "mov", "avi"])("uses the video icon for .%s", (extension) => {
		const icon = getProjectFileIconSrc(`video.${extension}`);
		expect(icon).toBe(getProjectFileIconSrc("video.mp4"));
		expect(icon).not.toBe(getProjectFileIconSrc("notes.txt"));
	});
});
