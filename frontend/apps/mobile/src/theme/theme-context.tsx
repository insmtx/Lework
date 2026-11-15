import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useColorScheme } from "react-native";

import { applySoundPreference, setForegroundSoundEnabled } from "../push/register";
import { DEFAULT_PREFERENCES, type Preferences } from "../settings/preferences";
import { createSecurePreferencesStore } from "../settings/secure-store";
import { type ColorScheme, resolveScheme, type Theme, type ThemeMode, themeFor } from "./theme";

/**
 * ThemePreference 是偏好读写与主题合成的统一入口。
 *
 * 名字沿用主题上下文：设置页最早只放了外观，现在同一份本地偏好里还包含
 * 声音与过程展示，但读取方仍然只需要一个 Provider。
 */
export interface ThemePreference {
	preferences: Preferences;
	/** 用户选择：system / light / dark。 */
	mode: ThemeMode;
	/** 实际生效的配色，system 模式下跟随系统。 */
	scheme: ColorScheme;
	theme: Theme;
	setMode: (mode: ThemeMode) => void;
	setPreference: <K extends keyof Preferences>(key: K, value: Preferences[K]) => void;
}

const ThemePreferenceContext = createContext<ThemePreference | null>(null);

/**
 * ThemeProvider 在根布局加载并持有本地偏好。
 *
 * 偏好从本地异步读出：首帧先用默认值渲染，读到存储值后立即切换，
 * 因此不会因为等待 IO 而白屏。
 */
export function ThemeProvider({ children }: { children: ReactNode }) {
	const systemScheme = useColorScheme();
	const store = useMemo(() => createSecurePreferencesStore(), []);
	const [preferences, setPreferences] = useState<Preferences>(DEFAULT_PREFERENCES);
	const preferencesRef = useRef(preferences);

	useEffect(() => {
		let cancelled = false;
		void store.load().then((loaded) => {
			if (cancelled) return;
			preferencesRef.current = loaded;
			setPreferences(loaded);
		});
		return () => {
			cancelled = true;
		};
	}, [store]);

	// 前台通知处理器不在 React 树里，声音偏好变化时单独同步一次。
	useEffect(() => {
		setForegroundSoundEnabled(preferences.soundEnabled);
	}, [preferences.soundEnabled]);

	const setPreference = useCallback(
		<K extends keyof Preferences>(key: K, value: Preferences[K]) => {
			const next = { ...preferencesRef.current, [key]: value };
			preferencesRef.current = next;
			// 先改内存再落盘：设置页点一下就该立刻生效，存储失败最多丢失偏好。
			setPreferences(next);
			void store.save(next).catch(() => {
				// 本地偏好落盘失败只影响下次启动，不值得为它打断操作。
			});
			if (key === "soundEnabled") {
				void applySoundPreference(Boolean(value)).catch(() => {
					// 渠道重建失败时仍保留偏好，下次启动或再次切换会重试。
				});
			}
		},
		[store],
	);

	const setMode = useCallback(
		(next: ThemeMode) => setPreference("themeMode", next),
		[setPreference],
	);

	const scheme = resolveScheme(preferences.themeMode, systemScheme);
	const value = useMemo<ThemePreference>(
		() => ({
			preferences,
			mode: preferences.themeMode,
			scheme,
			theme: themeFor(scheme),
			setMode,
			setPreference,
		}),
		[preferences, scheme, setMode, setPreference],
	);

	return (
		<ThemePreferenceContext.Provider value={value}>{children}</ThemePreferenceContext.Provider>
	);
}

/** useThemePreference 读取偏好；在 ThemeProvider 之外使用属于接线错误。 */
export function useThemePreference(): ThemePreference {
	const value = useContext(ThemePreferenceContext);
	if (!value) throw new Error("useThemePreference must be used inside ThemeProvider");
	return value;
}

/** useOptionalThemePreference 供 useTheme 兜底：没有 Provider 时退回系统配色。 */
export function useOptionalThemePreference(): ThemePreference | null {
	return useContext(ThemePreferenceContext);
}
