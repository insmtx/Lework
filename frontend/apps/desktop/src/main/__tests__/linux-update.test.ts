import { describe, expect, it } from "vitest";

import { isVersionNewer, parseLinuxUpdateMetadata } from "../linux-update";

describe("linux update metadata", () => {
	it("parses electron-builder latest-linux metadata", () => {
		expect(
			parseLinuxUpdateMetadata(`
version: 0.3.1
path: Lework-0.3.1-linux-amd64.deb
releaseDate: '2026-07-24T08:00:00.000Z'
`),
		).toEqual({
			version: "0.3.1",
			releaseDate: "2026-07-24T08:00:00.000Z",
		});
	});

	it("rejects metadata without a version", () => {
		expect(() => parseLinuxUpdateMetadata("path: Lework.deb")).toThrow(
			"Linux 更新元数据缺少 version",
		);
	});

	it("compares stable and prerelease versions", () => {
		expect(isVersionNewer("0.3.1", "0.3.0")).toBe(true);
		expect(isVersionNewer("0.3.0", "0.3.0")).toBe(false);
		expect(isVersionNewer("0.3.0", "0.3.1")).toBe(false);
		expect(isVersionNewer("0.3.1", "0.3.1-beta.2")).toBe(true);
		expect(isVersionNewer("0.3.1-beta.2", "0.3.1-beta.1")).toBe(true);
	});
});
