export const LEFT_RAIL_LIST_PREVIEW_LIMIT = 6;

export function getRecentProjectsForLeftRail<T extends { updatedAt: number }>(projects: T[]) {
	// 中文注释：最近项目按更新时间倒序展示当前已加载分页，后续页由滚动懒加载补齐。
	return [...projects].sort((a, b) => b.updatedAt - a.updatedAt);
}

export function getVisibleLeftRailItems<T>(
	items: T[],
	expanded: boolean,
	limit = LEFT_RAIL_LIST_PREVIEW_LIMIT,
) {
	const normalizedLimit = Math.max(0, limit);

	// 中文注释：侧栏默认只预览前 N 条，展开后再一次性返回完整列表。
	const visibleItems = expanded ? items : items.slice(0, normalizedLimit);

	return {
		visibleItems,
		showExpandTrigger: !expanded && items.length > normalizedLimit,
	};
}
