import { describe, expect, it } from "vitest";

import {
	createPreferencesStore,
	DEFAULT_PREFERENCES,
	FONT_SCALE,
	isThemeMode,
	normalizePreferences,
	PREFERENCES_KEY,
	type Preferences,
} from "./preferences";
import type { KeyValueStore } from "./settings";

function fakeStore(): KeyValueStore & { data: Map<string, string> } {
	const data = new Map<string, string>();
	return {
		data,
		getItem: async (key) => data.get(key) ?? null,
		setItem: async (key, value) => {
			data.set(key, value);
		},
		deleteItem: async (key) => {
			data.delete(key);
		},
	};
}

const custom: Preferences = {
	themeMode: "dark",
	soundEnabled: false,
	processDisplay: "full",
	toolDisplay: "expanded",
	fontSize: "large",
};

describe("normalizePreferences", () => {
	it("缺失字段逐项回落到默认值", () => {
		expect(normalizePreferences({})).toEqual(DEFAULT_PREFERENCES);
		expect(normalizePreferences(null)).toEqual(DEFAULT_PREFERENCES);
		expect(normalizePreferences({ themeMode: "dark" })).toEqual({
			...DEFAULT_PREFERENCES,
			themeMode: "dark",
		});
	});

	it("保留合法字段，丢弃非法枚举", () => {
		expect(
			normalizePreferences({
				themeMode: "dark",
				processDisplay: "verbose",
				toolDisplay: "expanded",
				fontSize: 42,
				soundEnabled: false,
			}),
		).toEqual({
			...DEFAULT_PREFERENCES,
			themeMode: "dark",
			toolDisplay: "expanded",
			soundEnabled: false,
		});
	});

	it("非对象输入整体回落到默认值", () => {
		expect(normalizePreferences("dark")).toEqual(DEFAULT_PREFERENCES);
		expect(normalizePreferences([])).toEqual(DEFAULT_PREFERENCES);
	});
});

describe("createPreferencesStore", () => {
	it("没有保存过时返回默认偏好", async () => {
		await expect(createPreferencesStore(fakeStore()).load()).resolves.toEqual(DEFAULT_PREFERENCES);
	});

	it("保存后可读回", async () => {
		const store = createPreferencesStore(fakeStore());
		await store.save(custom);
		await expect(store.load()).resolves.toEqual(custom);
	});

	it("使用独立的键，不与连接设置互相覆盖", async () => {
		const kv = fakeStore();
		await createPreferencesStore(kv).save(custom);
		expect(kv.data.has(PREFERENCES_KEY)).toBe(true);
	});

	it("内容损坏时回落到默认偏好", async () => {
		const kv = fakeStore();
		kv.data.set(PREFERENCES_KEY, "{not json");
		await expect(createPreferencesStore(kv).load()).resolves.toEqual(DEFAULT_PREFERENCES);
	});

	it("旧版本只有主题时补齐新字段", async () => {
		const kv = fakeStore();
		kv.data.set(PREFERENCES_KEY, JSON.stringify({ themeMode: "light" }));
		await expect(createPreferencesStore(kv).load()).resolves.toEqual({
			...DEFAULT_PREFERENCES,
			themeMode: "light",
		});
	});

	it("旧版本的 chatFontSize 迁移为 fontSize", async () => {
		const kv = fakeStore();
		kv.data.set(PREFERENCES_KEY, JSON.stringify({ themeMode: "light", chatFontSize: "large" }));
		await expect(createPreferencesStore(kv).load()).resolves.toEqual({
			...DEFAULT_PREFERENCES,
			themeMode: "light",
			fontSize: "large",
		});
	});
});

describe("isThemeMode", () => {
	it("只接受三个合法值", () => {
		expect(isThemeMode("system")).toBe(true);
		expect(isThemeMode("light")).toBe(true);
		expect(isThemeMode("dark")).toBe(true);
		expect(isThemeMode("auto")).toBe(false);
		expect(isThemeMode(undefined)).toBe(false);
	});
});

describe("FONT_SCALE", () => {
	it("三档整体上移：标准档大于基线，各档依次递增", () => {
		expect(FONT_SCALE.small).toBe(1);
		expect(FONT_SCALE.standard).toBeGreaterThan(FONT_SCALE.small);
		expect(FONT_SCALE.large).toBeGreaterThan(FONT_SCALE.standard);
	});
});
