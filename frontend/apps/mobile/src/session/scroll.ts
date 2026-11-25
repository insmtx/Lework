export interface ScrollMetrics {
	offsetY: number;
	contentHeight: number;
	viewportHeight: number;
}

/** 离底部多少像素以内仍算“贴底”，给流式布局的取整误差留余量。 */
const defaultBottomThreshold = 48;

/**
 * isNearBottom 判断用户是否停在消息流底部。
 *
 * 流式回复会不断增高内容；只有贴底的用户才跟着滚，向上翻历史时不能把他
 * 拽回最新一条。内容不足一屏时剩余距离为负，同样视为贴底。
 */
export function isNearBottom(
	metrics: ScrollMetrics,
	threshold: number = defaultBottomThreshold,
): boolean {
	const remaining = metrics.contentHeight - (metrics.offsetY + metrics.viewportHeight);
	return remaining <= threshold;
}

/**
 * isFarFromBottom 判断是否离底部超过半屏，用于露出「回到底部」按钮。
 *
 * 比贴底判定宽得多：小幅上滑就弹按钮会挡住内容，也会让人误以为消息流断了。
 */
export function isFarFromBottom(metrics: ScrollMetrics): boolean {
	return !isNearBottom(metrics, metrics.viewportHeight / 2);
}

/**
 * bottomOffset 返回贴底时应滚动到的偏移。
 *
 * 用真实内容高度与视口高度算，而不是 FlatList.scrollToEnd：后者在没有
 * getItemLayout 的变高列表里按平均行高估算末尾，流式内容变化时会越滚越偏。
 */
export function bottomOffset(contentHeight: number, viewportHeight: number): number {
	return Math.max(0, contentHeight - viewportHeight);
}

/**
 * followOffset 返回为贴底需要滚动到的偏移；已经贴底或内容回缩时返回 null。
 *
 * 只向底部追（偏移增大），内容变短交给系统把偏移夹回新的底部。流式期间
 * 内容高度会因虚拟列表重新测量而小幅上下浮动，如果两个方向都主动滚，
 * 就会变成持续抖动。
 */
export function followOffset(target: number, currentOffset: number): number | null {
	return target > currentOffset + 0.5 ? target : null;
}
