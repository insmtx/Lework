import { describe, expect, it } from "vitest";

import { DEFAULT_PREFERENCES, type Preferences } from "./preferences";
import {
	describeAccount,
	describeAppearance,
	describeProcess,
	describeReminder,
	describeServer,
} from "./summary";

const prefs: Preferences = {
	...DEFAULT_PREFERENCES,
	themeMode: "dark",
	fontSize: "large",
	processDisplay: "full",
	toolDisplay: "expanded",
	soundEnabled: false,
};

describe("describeAppearance", () => {
	it("组合主题与字号", () => {
		expect(describeAppearance(DEFAULT_PREFERENCES)).toBe("跟随系统 · 标准字号");
		expect(describeAppearance(prefs)).toBe("暗色 · 大字号");
	});
});

describe("describeProcess", () => {
	it("组合思考过程与工具调用", () => {
		expect(describeProcess(DEFAULT_PREFERENCES)).toBe("思考摘要 · 工具折叠");
		expect(describeProcess(prefs)).toBe("思考完整 · 工具展开");
	});

	it("隐藏时仍然可读", () => {
		expect(
			describeProcess({
				...DEFAULT_PREFERENCES,
				processDisplay: "hidden",
				toolDisplay: "hidden",
			}),
		).toBe("思考隐藏 · 工具隐藏");
	});
});

describe("describeReminder", () => {
	it("未连接时优先说明连接状态", () => {
		expect(describeReminder(DEFAULT_PREFERENCES, true, false)).toBe("未连接");
	});

	it("推送关闭时仍展示声音开关", () => {
		expect(describeReminder(DEFAULT_PREFERENCES, false, true)).toBe("推送关闭 · 声音开启");
		expect(describeReminder(prefs, false, true)).toBe("推送关闭 · 声音关闭");
	});

	it("推送开启时附带声音开关", () => {
		expect(describeReminder(DEFAULT_PREFERENCES, true, true)).toBe("推送开启 · 声音开启");
		expect(describeReminder(prefs, true, true)).toBe("推送开启 · 声音关闭");
	});
});

describe("describeServer", () => {
	it("已连接时显示地址", () => {
		expect(describeServer("https://box:8787")).toBe("https://box:8787");
	});

	it("没有地址或空串时显示未连接", () => {
		expect(describeServer(undefined)).toBe("未连接");
		expect(describeServer("")).toBe("未连接");
	});
});

describe("describeAccount", () => {
	it("显示邮箱或登录入口", () => {
		expect(describeAccount("a@example.com")).toBe("a@example.com");
		expect(describeAccount("")).toBe("登录 / 注册");
		expect(describeAccount(undefined)).toBe("登录 / 注册");
	});
});
