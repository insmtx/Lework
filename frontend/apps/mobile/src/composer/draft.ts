// 草稿按「项目 + 节点」隔离：同一个项目在不同节点上是不同工作副本，
// 共用一份草稿会让用户把本地目录的指令发到另一台机器上。

/** DraftScope 标识草稿归属的项目与节点。 */
export interface DraftScope {
	projectKey: string;
	nodeID: string;
}

/** DraftKV 是草稿持久化的最小接口，便于在测试里注入内存实现。 */
export interface DraftKV {
	getItem(key: string): Promise<string | null>;
	setItem(key: string, value: string): Promise<void>;
	deleteItem(key: string): Promise<void>;
}

/**
 * draftStorageKey 生成稳定的存储键；换 scope 不会互相覆盖。
 *
 * SecureStore 的键只允许字母数字与 ._-，而项目 key 可能是
 * "global:/abs/path"，因此逐码点转十六进制：稳定、无冲突且合法。
 */
export function draftStorageKey(scope: DraftScope): string {
	return `lework.draft.v1.${encodeScopePart(scope.projectKey)}.${encodeScopePart(scope.nodeID)}`;
}

function encodeScopePart(value: string): string {
	return Array.from(value)
		.map((char) => char.codePointAt(0)?.toString(16) ?? "")
		.join("");
}

export interface DraftStore {
	load(scope: DraftScope): Promise<string>;
	save(scope: DraftScope, text: string): Promise<void>;
}

/**
 * createDraftStore 把输入框内容存进注入的 kv（移动端是 secure-store）。
 *
 * 空白文本直接删除键，避免留下一条永远清不掉的“空草稿”；读取时把
 * null 归一成空串，调用方不必区分“没写过”和“写空过”。
 */
export function createDraftStore(kv: DraftKV): DraftStore {
	return {
		async load(scope) {
			const value = await kv.getItem(draftStorageKey(scope));
			return value ?? "";
		},
		async save(scope, text) {
			const key = draftStorageKey(scope);
			if (text.trim() === "") {
				await kv.deleteItem(key);
				return;
			}
			await kv.setItem(key, text);
		},
	};
}
