import { createContext, type ReactNode, useContext, useMemo } from "react";

import { useTheme } from "../../theme/use-theme";
import { createStyles } from "./styles";

const ToolFontScaleContext = createContext(1);

/**
 * ToolFontScaleProvider 把会话字号倍率传给卡片、块渲染器和弹层。
 *
 * 用 context 而不是逐层传参：同一次展开里还有列表、diff、原始数据等子组件，
 * 每个都加一个 fontScale prop 既啰嗦又容易漏掉。
 */
export function ToolFontScaleProvider({ scale, children }: { scale: number; children: ReactNode }) {
	return <ToolFontScaleContext.Provider value={scale}>{children}</ToolFontScaleContext.Provider>;
}

/** useToolStyles 返回随主题与当前会话字号缓存的样式表。 */
export function useToolStyles() {
	const scale = useContext(ToolFontScaleContext);
	const theme = useTheme();
	return useMemo(() => createStyles(theme, scale), [theme, scale]);
}
