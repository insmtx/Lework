import { describe, expect, it } from "vitest";
import { createDraftStore, draftStorageKey, type DraftKV } from "./draft";

function memoryKV(): DraftKV & { values: Map<string, string> } {
	const values = new Map<string, string>();
	return {
		values,
		async getItem(key) {
			return values.get(key) ?? null;
		},
		async setItem(key, value) {
			values.set(key, value);
		},
		async deleteItem(key) {
			values.delete(key);
		},
	};
}

describe("draftStorageKey", () => {
	it("只包含 SecureStore 允许的字符，且不同 scope 互不覆盖", () => {
		const key = draftStorageKey({ projectKey: "global:/w/repo", nodeID: "n1" });
		expect(key).toMatch(/^[a-zA-Z0-9._-]+$/);
		expect(draftStorageKey({ projectKey: "abc", nodeID: "n1" })).not.toBe(
			draftStorageKey({ projectKey: "abc", nodeID: "n2" }),
		);
	});
});

describe("createDraftStore", () => {
	it("保存后能读回同一 scope 的文本", async () => {
		const kv = memoryKV();
		const store = createDraftStore(kv);
		await store.save({ projectKey: "abc", nodeID: "n1" }, "fix the tests");
		await expect(store.load({ projectKey: "abc", nodeID: "n1" })).resolves.toBe("fix the tests");
		await expect(store.load({ projectKey: "abc", nodeID: "n2" })).resolves.toBe("");
	});

	it("空白文本会删除存储键", async () => {
		const kv = memoryKV();
		const store = createDraftStore(kv);
		await store.save({ projectKey: "abc", nodeID: "n1" }, "draft");
		await store.save({ projectKey: "abc", nodeID: "n1" }, "   ");
		expect(kv.values.size).toBe(0);
	});
});
