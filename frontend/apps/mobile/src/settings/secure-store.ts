import * as SecureStore from "expo-secure-store";

import { createPreferencesStore, type PreferencesStore } from "./preferences";
import { createSettingsStore, type KeyValueStore, type SettingsStore } from "./settings";

// SecureStore 的键只允许字母数字与 ._-；SETTINGS_KEY 与 PREFERENCES_KEY 均满足。
const adapter: KeyValueStore = {
	getItem: (key) => SecureStore.getItemAsync(key),
	setItem: (key, value) => SecureStore.setItemAsync(key, value),
	deleteItem: (key) => SecureStore.deleteItemAsync(key),
};

export function createSecureSettingsStore(): SettingsStore {
	return createSettingsStore(adapter);
}

/** createSecurePreferencesStore 复用同一适配器保存外观等非敏感偏好。 */
export function createSecurePreferencesStore(): PreferencesStore {
	return createPreferencesStore(adapter);
}
