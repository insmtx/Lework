import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useState,
} from "react";
import { useColorScheme } from "react-native";

import { createSecurePreferencesStore } from "../settings/secure-store";
import { type ColorScheme, resolveScheme, type Theme, type ThemeMode, themeFor } from "./theme";

/** ThemePreference 是主题偏好的读写入口，设置在设置页里改。 */
export interface ThemePreference {
	/** 用户选择：system / light / dark。 */
	mode: ThemeMode;
	/** 实际生效的配色，system 模式下跟随系统。 */
	scheme: ColorScheme;
	theme: Theme;
	setMode: (mode: ThemeMode) => void;
}

const ThemePreferenceContext = createContext<ThemePreference | null>(null);

/**
 * ThemeProvider 在根布局加载并持有外观偏好。
 *
 * 偏好从本地异步读出：首帧先用 system 渲染，读到存储值后立即切换，
 * 因此不会因为等待 IO 而白屏。
 */
export function ThemeProvider({ children }: { children: ReactNode }) {
	const systemScheme = useColorScheme();
	const store = useMemo(() => createSecurePreferencesStore(), []);
	const [mode, setModeState] = useState<ThemeMode>("system");

	useEffect(() => {
		let cancelled = false;
		void store.load().then((preferences) => {
			if (!cancelled) setModeState(preferences.themeMode);
		});
		return () => {
			cancelled = true;
		};
	}, [store]);

	const setMode = useCallback(
		(next: ThemeMode) => {
			// 先改内存再落盘：设置页点一下就该立刻变色，存储失败最多丢失偏好，
			// 不值得让 UI 等 IO 或弹错误。
			setModeState(next);
			void store.save({ themeMode: next });
		},
		[store],
	);

	const scheme = resolveScheme(mode, systemScheme);
	const value = useMemo<ThemePreference>(
		() => ({ mode, scheme, theme: themeFor(scheme), setMode }),
		[mode, scheme, setMode],
	);

	return (
		<ThemePreferenceContext.Provider value={value}>{children}</ThemePreferenceContext.Provider>
	);
}

/** useThemePreference 读取主题偏好；在 ThemeProvider 之外使用属于接线错误。 */
export function useThemePreference(): ThemePreference {
	const value = useContext(ThemePreferenceContext);
	if (!value) throw new Error("useThemePreference must be used inside ThemeProvider");
	return value;
}

/** useOptionalThemePreference 供 useTheme 兜底：没有 Provider 时退回系统配色。 */
export function useOptionalThemePreference(): ThemePreference | null {
	return useContext(ThemePreferenceContext);
}
