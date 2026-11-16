import { describe, expect, it } from "vitest";
import { compareVersions, isVersionNewer } from "./version";

describe("version", () => {
	it("compares stable versions", () => {
		expect(compareVersions("0.8.0", "0.7.9")).toBe(1);
		expect(compareVersions("0.8.0", "0.8.0")).toBe(0);
		expect(compareVersions("0.7.9", "0.8.0")).toBe(-1);
	});

	it("treats prerelease as older than stable", () => {
		expect(isVersionNewer("0.8.0", "0.8.0-rc.1")).toBe(true);
		expect(isVersionNewer("0.8.0-rc.1", "0.8.0")).toBe(false);
		expect(isVersionNewer("0.8.0-rc.2", "0.8.0-rc.1")).toBe(true);
	});

	it("rejects invalid versions", () => {
		expect(() => compareVersions("latest", "0.8.0")).toThrow();
	});
});
