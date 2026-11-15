import { useMemo } from "react";
import { useColorScheme } from "react-native";

import { FONT_SCALE } from "../settings/preferences";
import { scaleFontStyles } from "./font-scale";
import type { Theme } from "./theme";
import { themeFor } from "./theme";
import { useOptionalThemePreference } from "./theme-context";

/**
 * useTheme 返回当前生效的色板。
 *
 * 设置里选了亮/暗就以偏好为准，否则跟随系统；没有 ThemeProvider 时退回
 * 系统配色，让单独渲染的组件仍能拿到合理的颜色。
 */
export function useTheme(): Theme {
	const preference = useOptionalThemePreference();
	const systemScheme = useColorScheme();
	return preference ? preference.theme : themeFor(systemScheme);
}

/**
 * useFontScale 返回全局字号倍率。
 *
 * 没有 ThemeProvider 时按基线 1 处理：单独渲染的组件不该意外放大。
 */
export function useFontScale(): number {
	const preference = useOptionalThemePreference();
	return preference ? FONT_SCALE[preference.preferences.fontSize] : 1;
}

/**
 * useThemedStyles 把「色板 → StyleSheet」的工厂缓存起来，并统一缩放字号。
 *
 * 工厂必须定义在模块顶层（引用稳定），否则每次渲染都会重建样式表。
 * 所有页面都通过这里取样式，字号偏好才能覆盖标题、状态和正文，而不是
 * 只作用于会话内容。
 */
export function useThemedStyles<T>(factory: (theme: Theme) => T): T {
	const theme = useTheme();
	const fontScale = useFontScale();
	return useMemo(() => scaleFontStyles(factory(theme), fontScale), [factory, theme, fontScale]);
}
