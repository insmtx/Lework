import { describe, expect, it } from "vitest";
import {
	DESKTOP_UPDATE_BASE_URL,
	desktopTarget,
	feedUrl,
	latestTxtUrl,
	metadataUrl,
	parseLatestVersion,
} from "../update-source";

describe("desktop update source", () => {
	it("parses LATEST.txt and rejects html", () => {
		expect(parseLatestVersion("0.8.0\n")).toBe("0.8.0");
		expect(() => parseLatestVersion("")).toThrow();
		expect(() => parseLatestVersion("<html>error</html>")).toThrow();
	});

	it("maps supported platform/arch to version directories", () => {
		expect(desktopTarget("darwin", "arm64")).toEqual({
			dir: "darwin/arm64",
			metadataFile: "latest-mac.yml",
		});
		expect(desktopTarget("darwin", "x64")?.dir).toBe("darwin/x64");
		expect(desktopTarget("win32", "x64")).toEqual({
			dir: "win32/x64",
			metadataFile: "latest.yml",
		});
		expect(desktopTarget("linux", "x64")).toEqual({
			dir: "linux/x64",
			metadataFile: "latest-linux.yml",
		});
		expect(desktopTarget("linux", "arm64")).toBeNull();
		expect(desktopTarget("freebsd", "x64")).toBeNull();
	});

	it("builds cache-busted urls", () => {
		const target = desktopTarget("darwin", "arm64")!;
		const feed = `${DESKTOP_UPDATE_BASE_URL}/0.8.0/darwin/arm64`;
		expect(feedUrl(DESKTOP_UPDATE_BASE_URL, "0.8.0", target)).toBe(feed);
		expect(metadataUrl(DESKTOP_UPDATE_BASE_URL, "0.8.0", target, 123)).toBe(
			`${feed}/latest-mac.yml?t=123`,
		);
		expect(latestTxtUrl(DESKTOP_UPDATE_BASE_URL, 123)).toBe(
			`${DESKTOP_UPDATE_BASE_URL}/LATEST.txt?t=123`,
		);
	});
});
