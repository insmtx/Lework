import { describe, expect, it } from "vitest";
import {
	createSettingsStore,
	normalizeBaseUrl,
	type KeyValueStore,
	type Settings,
} from "./settings";

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

describe("normalizeBaseUrl", () => {
	it("去除空白与末尾斜杠", () => {
		expect(normalizeBaseUrl(" http://box:8787/ ")).toBe("http://box:8787");
	});

	it("保留子路径", () => {
		expect(normalizeBaseUrl("https://example.com/cp/")).toBe("https://example.com/cp");
	});

	it("拒绝空串与非 http(s)", () => {
		expect(() => normalizeBaseUrl(" ")).toThrow();
		expect(() => normalizeBaseUrl("ftp://box")).toThrow();
		expect(() => normalizeBaseUrl("not-a-url")).toThrow();
	});
});

describe("createSettingsStore", () => {
	const settings: Settings = {
		baseUrl: "http://box:8787",
		token: "secret",
		notifications: true,
	};

	it("保存后可读回", async () => {
		const store = createSettingsStore(fakeStore());
		await store.save(settings);
		await expect(store.load()).resolves.toEqual(settings);
	});

	it("缺失或损坏时返回 null", async () => {
		await expect(createSettingsStore(fakeStore()).load()).resolves.toBeNull();

		const kv = fakeStore();
		await kv.setItem("lework.settings.v1", "{not json");
		await expect(createSettingsStore(kv).load()).resolves.toBeNull();
	});

	it("clear 后为空", async () => {
		const store = createSettingsStore(fakeStore());
		await store.save(settings);
		await store.clear();
		await expect(store.load()).resolves.toBeNull();
	});
});
