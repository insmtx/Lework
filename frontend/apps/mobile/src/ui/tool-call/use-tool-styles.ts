import { useThemedStyles } from "../../theme/use-theme";
import { createStyles } from "./styles";

/** useToolStyles 返回随主题与全局字号缓存的工具卡片样式表。 */
export function useToolStyles() {
	return useThemedStyles(createStyles);
}
