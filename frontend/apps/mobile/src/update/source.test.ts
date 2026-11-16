import { describe, expect, it } from "vitest";
import { apkUrl, MOBILE_UPDATE_BASE_URL, parseReleaseVersion, releaseTxtUrl } from "./source";

describe("update source", () => {
	it("parses the first line of RELEASE.txt", () => {
		expect(parseReleaseVersion("0.8.0\n")).toBe("0.8.0");
		expect(parseReleaseVersion("\n 0.8.0 \n")).toBe("0.8.0");
	});

	it("rejects empty or html bodies", () => {
		expect(() => parseReleaseVersion("")).toThrow();
		expect(() => parseReleaseVersion("<html>error</html>")).toThrow();
	});

	it("builds cache-busted release and apk urls", () => {
		expect(MOBILE_UPDATE_BASE_URL).toBe(
			"https://leros-1395325824.cos.ap-beijing.myqcloud.com/application",
		);
		expect(releaseTxtUrl(MOBILE_UPDATE_BASE_URL, 1700000000000)).toBe(
			`${MOBILE_UPDATE_BASE_URL}/RELEASE.txt?t=1700000000000`,
		);
		expect(apkUrl("0.8.0")).toBe(
			`${MOBILE_UPDATE_BASE_URL}/0.8.0/android/lework-android-0.8.0.apk`,
		);
	});
});
