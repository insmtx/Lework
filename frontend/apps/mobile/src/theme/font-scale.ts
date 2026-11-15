/** 只缩放文字尺寸；布局尺寸（宽高、内边距）保持原样，避免「字号」变成整体缩放。 */
const FONT_KEYS = ["fontSize", "lineHeight"] as const;

/**
 * scaleFontStyles 把样式表里每一处字号与行高乘以同一倍率。
 *
 * React Native 没有全局字号设置，逐个组件传 fontScale 很容易漏掉标题、状态、
 * 思考过程这类固定文本。统一的缩放点放在 useThemedStyles 里，样式表仍按主题
 * 构建，只在返回前整体缩放文字尺寸。
 */
export function scaleFontStyles<T>(styles: T, scale: number): T {
	if (scale === 1 || typeof styles !== "object" || styles === null) return styles;

	const scaled: Record<string, unknown> = {};
	for (const [key, value] of Object.entries(styles as Record<string, unknown>)) {
		if (!isStyleRecord(value)) {
			scaled[key] = value;
			continue;
		}

		const next: Record<string, unknown> = { ...value };
		for (const field of FONT_KEYS) {
			const current = next[field];
			if (typeof current === "number") next[field] = Math.round(current * scale);
		}
		scaled[key] = next;
	}

	return scaled as T;
}

function isStyleRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
