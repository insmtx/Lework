import { useMemo } from "react";
import { useColorScheme } from "react-native";

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
 * useThemedStyles 把「色板 → StyleSheet」的工厂缓存起来。
 *
 * 工厂必须定义在模块顶层（引用稳定），否则每次渲染都会重建样式表。
 */
export function useThemedStyles<T>(factory: (theme: Theme) => T): T {
	const theme = useTheme();
	return useMemo(() => factory(theme), [factory, theme]);
}
