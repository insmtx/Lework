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
