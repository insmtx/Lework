import * as SecureStore from "expo-secure-store";

import { createDraftStore, type DraftStore } from "./draft";

/**
 * createSecureDraftStore 用系统安全存储保存草稿。
 *
 * 不引入 AsyncStorage：APK 里没有新的原生模块，secure-store 已经安装。
 * 草稿量很小，键链的读写开销可以接受。
 */
export function createSecureDraftStore(): DraftStore {
	return createDraftStore({
		getItem: (key) => SecureStore.getItemAsync(key),
		setItem: (key, value) => SecureStore.setItemAsync(key, value),
		deleteItem: (key) => SecureStore.deleteItemAsync(key),
	});
}
