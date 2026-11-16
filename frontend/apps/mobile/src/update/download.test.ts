import { describe, expect, it } from "vitest";
import { clampProgress, updateApkPath } from "./download";

describe("download helpers", () => {
	it("builds a versioned cache path", () => {
		expect(updateApkPath("/cache/", "0.8.0")).toBe("/cache/lework-update-0.8.0.apk");
		expect(updateApkPath("/cache", "0.8.0")).toBe("/cache/lework-update-0.8.0.apk");
	});

	it("clamps progress", () => {
		expect(clampProgress(-5)).toBe(0);
		expect(clampProgress(42.6)).toBe(43);
		expect(clampProgress(150)).toBe(100);
	});
});
