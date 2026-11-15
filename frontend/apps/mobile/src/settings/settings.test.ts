import { describe, expect, it } from "vitest";
import {
	SETTINGS_KEY,
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
		email: "a@example.com",
		sessionToken: "secret",
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

	it("旧设置（baseUrl + token）迁移为未登录状态", async () => {
		const kv = fakeStore();
		await kv.setItem(
			SETTINGS_KEY,
			JSON.stringify({ baseUrl: "http://box:8787", token: "admin-token", notifications: true }),
		);
		const loaded = await createSettingsStore(kv).load();
		expect(loaded).toEqual({
			baseUrl: "http://box:8787",
			email: "",
			sessionToken: "",
			notifications: true,
			deviceID: undefined,
		});
		// 迁移必须落盘：部署级令牌不能继续留在手机存储里。
		const raw = kv.data.get(SETTINGS_KEY) ?? "";
		expect(raw).not.toContain("admin-token");
		expect(JSON.parse(raw)).toEqual(loaded);
	});

	it("保存并读回登录态", async () => {
		const kv = fakeStore();
		const store = createSettingsStore(kv);
		const settings: Settings = {
			baseUrl: "http://box:8787",
			email: "a@example.com",
			sessionToken: "sess",
			notifications: true,
		};
		await store.save(settings);
		expect(await store.load()).toEqual(settings);
	});

	it("clear 后为空", async () => {
		const store = createSettingsStore(fakeStore());
		await store.save(settings);
		await store.clear();
		await expect(store.load()).resolves.toBeNull();
	});
});
