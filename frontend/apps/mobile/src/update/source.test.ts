import { describe, expect, it } from "vitest";
import { latestTxtUrl, MOBILE_UPDATE_BASE_URL, packageUrl, parseLatestVersion } from "./source";

describe("update source", () => {
	it("parses the first line of LATEST.txt", () => {
		expect(parseLatestVersion("0.8.0\n")).toBe("0.8.0");
		expect(parseLatestVersion("\n 0.8.0 \n")).toBe("0.8.0");
	});

	it("rejects empty or html bodies", () => {
		expect(() => parseLatestVersion("")).toThrow();
		expect(() => parseLatestVersion("<html>error</html>")).toThrow();
	});

	it("builds cache-busted release and package urls", () => {
		expect(MOBILE_UPDATE_BASE_URL).toBe(
			"https://leros-1395325824.cos.ap-beijing.myqcloud.com/application",
		);
		expect(latestTxtUrl(MOBILE_UPDATE_BASE_URL, 1700000000000)).toBe(
			`${MOBILE_UPDATE_BASE_URL}/LATEST.txt?t=1700000000000`,
		);
		// COS 不允许直接下载 apk，发布产物改为同名 zip，App 下载后再本地解压。
		expect(packageUrl("0.8.0")).toBe(
			`${MOBILE_UPDATE_BASE_URL}/0.8.0/android/lework-android-0.8.0.zip`,
		);
	});
});
