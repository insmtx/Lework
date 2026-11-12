import * as SecureStore from "expo-secure-store";

import { createSettingsStore, type KeyValueStore, type SettingsStore } from "./settings";

// SecureStore 的键只允许字母数字与 ._-；SETTINGS_KEY 已满足。
const adapter: KeyValueStore = {
	getItem: (key) => SecureStore.getItemAsync(key),
	setItem: (key, value) => SecureStore.setItemAsync(key, value),
	deleteItem: (key) => SecureStore.deleteItemAsync(key),
};

export function createSecureSettingsStore(): SettingsStore {
	return createSettingsStore(adapter);
}
