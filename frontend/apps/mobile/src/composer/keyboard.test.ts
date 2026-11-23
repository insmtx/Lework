import { describe, expect, it } from "vitest";

import { keyboardOverlap } from "./keyboard";

describe("keyboardOverlap", () => {
	it("键盘不可见时不额外留白", () => {
		expect(
			keyboardOverlap({ keyboardHeight: undefined, baseBottom: 24, platform: "android" }),
		).toBe(0);
	});

	it("Android 的键盘高度已扣掉导航栏，再整段补上", () => {
		expect(keyboardOverlap({ keyboardHeight: 300, baseBottom: 24, platform: "android" })).toBe(300);
	});

	it("iOS 的键盘高度已含底部安全区，只补差额", () => {
		expect(keyboardOverlap({ keyboardHeight: 300, baseBottom: 34, platform: "ios" })).toBe(266);
	});

	it("键盘比安全区还矮时不再上移", () => {
		expect(keyboardOverlap({ keyboardHeight: 20, baseBottom: 34, platform: "ios" })).toBe(0);
	});

	it("高度为 0 视为不可见", () => {
		expect(keyboardOverlap({ keyboardHeight: 0, baseBottom: 24, platform: "android" })).toBe(0);
	});
});
