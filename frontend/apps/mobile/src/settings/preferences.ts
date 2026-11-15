import type { ThemeMode } from "../theme/theme";
import type { KeyValueStore } from "./settings";

// 外观偏好与连接设置分开保存：清空配对或换服务器不该把主题重置，
// 反过来主题也不属于需要进 keystore 的敏感信息。
export interface Preferences {
	themeMode: ThemeMode;
}

export interface PreferencesStore {
	load(): Promise<Preferences>;
	save(preferences: Preferences): Promise<void>;
}

export const PREFERENCES_KEY = "lework.preferences.v1";

export const DEFAULT_PREFERENCES: Preferences = { themeMode: "system" };

export function isThemeMode(value: unknown): value is ThemeMode {
	return value === "system" || value === "light" || value === "dark";
}

// createPreferencesStore 与设置存储共用 KeyValueStore 抽象，
// 纯逻辑可在没有 SecureStore 的测试里直接跑。
export function createPreferencesStore(kv: KeyValueStore): PreferencesStore {
	return {
		async load() {
			const raw = await kv.getItem(PREFERENCES_KEY);
			if (!raw) return DEFAULT_PREFERENCES;
			try {
				const parsed = JSON.parse(raw) as Partial<Preferences>;
				// 旧版本或手改过的值一律回落到跟随系统，避免无效偏好导致页面异常。
				return { themeMode: isThemeMode(parsed.themeMode) ? parsed.themeMode : "system" };
			} catch {
				return DEFAULT_PREFERENCES;
			}
		},

		save(preferences) {
			return kv.setItem(PREFERENCES_KEY, JSON.stringify(preferences));
		},
	};
}
