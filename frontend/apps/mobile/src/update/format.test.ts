import { describe, expect, it } from "vitest";
import { describeVersion } from "./format";

describe("describeVersion", () => {
	it("shows the current version", () => {
		expect(describeVersion("0.8.0", { phase: "idle" })).toBe("0.8.0");
		expect(describeVersion(null, { phase: "idle" })).toBe("未知");
	});
	it("shows update state when available", () => {
		expect(describeVersion("0.8.0", { phase: "available", version: "0.9.0", apkUrl: "u" })).toBe(
			"0.9.0 可更新",
		);
		expect(
			describeVersion("0.8.0", {
				phase: "downloading",
				version: "0.9.0",
				apkUrl: "u",
				progress: 10,
			}),
		).toBe("0.9.0 下载中");
	});
});
