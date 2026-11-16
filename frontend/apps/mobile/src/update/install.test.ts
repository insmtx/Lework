import { describe, expect, it } from "vitest";
import { buildInstallIntent } from "./install";

describe("buildInstallIntent", () => {
	it("opens the package installer with a content uri", () => {
		expect(buildInstallIntent("content://com.insmtx.lework.fileprovider/cache/a.apk")).toEqual({
			action: "android.intent.action.VIEW",
			params: {
				data: "content://com.insmtx.lework.fileprovider/cache/a.apk",
				type: "application/vnd.android.package-archive",
				flags: 1,
			},
		});
	});
});
