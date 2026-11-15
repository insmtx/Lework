import { describe, expect, it } from "vitest";
import {
	createPreferencesStore,
	DEFAULT_PREFERENCES,
	isThemeMode,
	PREFERENCES_KEY,
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

describe("createPreferencesStore", () => {
	it("没有保存过时返回默认的跟随系统", async () => {
		const store = createPreferencesStore(fakeStore());
		await expect(store.load()).resolves.toEqual(DEFAULT_PREFERENCES);
	});

	it("保存后可读回", async () => {
		const store = createPreferencesStore(fakeStore());
		await store.save({ themeMode: "dark" });
		await expect(store.load()).resolves.toEqual({ themeMode: "dark" });
	});

	it("使用独立的键，不与连接设置互相覆盖", async () => {
		const kv = fakeStore();
		await createPreferencesStore(kv).save({ themeMode: "light" });
		expect(kv.data.has(PREFERENCES_KEY)).toBe(true);
	});

	it("内容损坏或主题值非法时回落到跟随系统", async () => {
		const kv = fakeStore();
		kv.data.set(PREFERENCES_KEY, "{not json");
		await expect(createPreferencesStore(kv).load()).resolves.toEqual(DEFAULT_PREFERENCES);

		kv.data.set(PREFERENCES_KEY, JSON.stringify({ themeMode: "purple" }));
		await expect(createPreferencesStore(kv).load()).resolves.toEqual(DEFAULT_PREFERENCES);
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
