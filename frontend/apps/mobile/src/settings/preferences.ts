import type { ThemeMode } from "../theme/theme";
import type { KeyValueStore } from "./settings";

/**
 * ProcessDisplay 控制执行过程中「思考过程」的展示程度。
 *
 * hidden 完全不展示，brief 只给一段摘要，full 直接展开完整思考。
 */
export type ProcessDisplay = "hidden" | "brief" | "full";

/** ToolDisplay 控制工具调用卡片的默认形态。 */
export type ToolDisplay = "hidden" | "collapsed" | "expanded";

/** ChatFontSize 只影响会话正文，避免把导航和按钮一起放大。 */
export type ChatFontSize = "small" | "standard" | "large";

/**
 * Preferences 是与连接无关的本地偏好。
 *
 * 与 Settings 分开保存：清空配对或换服务器不该重置外观与展示习惯，
 * 这些值也不属于需要进 keystore 的敏感信息。
 */
export interface Preferences {
	themeMode: ThemeMode;
	soundEnabled: boolean;
	processDisplay: ProcessDisplay;
	toolDisplay: ToolDisplay;
	chatFontSize: ChatFontSize;
}

export interface PreferencesStore {
	load(): Promise<Preferences>;
	save(preferences: Preferences): Promise<void>;
}

export const PREFERENCES_KEY = "lework.preferences.v1";

export const DEFAULT_PREFERENCES: Preferences = {
	themeMode: "system",
	soundEnabled: true,
	processDisplay: "brief",
	toolDisplay: "collapsed",
	chatFontSize: "standard",
};

/**
 * CHAT_FONT_SCALE 把字号偏好映射成正文的放大倍率。
 *
 * 用倍率而不是三套固定字号：Markdown 里标题、代码块各有自己的尺寸，
 * 倍率能一次性整体缩放，不用为每种元素再写一张表。
 */
export const CHAT_FONT_SCALE: Record<ChatFontSize, number> = {
	small: 0.9,
	standard: 1,
	large: 1.15,
};

const THEME_MODES: readonly ThemeMode[] = ["system", "light", "dark"];
const PROCESS_DISPLAYS: readonly ProcessDisplay[] = ["hidden", "brief", "full"];
const TOOL_DISPLAYS: readonly ToolDisplay[] = ["hidden", "collapsed", "expanded"];
const CHAT_FONT_SIZES: readonly ChatFontSize[] = ["small", "standard", "large"];

export function isThemeMode(value: unknown): value is ThemeMode {
	return typeof value === "string" && (THEME_MODES as readonly string[]).includes(value);
}

function pick<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
	return typeof value === "string" && (allowed as readonly string[]).includes(value)
		? (value as T)
		: fallback;
}

/**
 * normalizePreferences 把任意来源的旧/残缺偏好补齐。
 *
 * 手改存储或旧版本都可能缺字段，逐项校验比整体丢弃更友好：用户只丢
 * 掉真正非法的那个值，其它偏好仍然保留。
 */
export function normalizePreferences(raw: unknown): Preferences {
	if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
		return DEFAULT_PREFERENCES;
	}

	const parsed = raw as Partial<Record<keyof Preferences, unknown>>;
	return {
		themeMode: pick(parsed.themeMode, THEME_MODES, DEFAULT_PREFERENCES.themeMode),
		soundEnabled:
			typeof parsed.soundEnabled === "boolean"
				? parsed.soundEnabled
				: DEFAULT_PREFERENCES.soundEnabled,
		processDisplay: pick(
			parsed.processDisplay,
			PROCESS_DISPLAYS,
			DEFAULT_PREFERENCES.processDisplay,
		),
		toolDisplay: pick(parsed.toolDisplay, TOOL_DISPLAYS, DEFAULT_PREFERENCES.toolDisplay),
		chatFontSize: pick(parsed.chatFontSize, CHAT_FONT_SIZES, DEFAULT_PREFERENCES.chatFontSize),
	};
}

// createPreferencesStore 与设置存储共用 KeyValueStore 抽象，
// 纯逻辑可在没有 SecureStore 的测试里直接跑。
export function createPreferencesStore(kv: KeyValueStore): PreferencesStore {
	return {
		async load() {
			const raw = await kv.getItem(PREFERENCES_KEY);
			if (!raw) return DEFAULT_PREFERENCES;
			try {
				return normalizePreferences(JSON.parse(raw));
			} catch {
				return DEFAULT_PREFERENCES;
			}
		},

		save(preferences) {
			return kv.setItem(PREFERENCES_KEY, JSON.stringify(preferences));
		},
	};
}
