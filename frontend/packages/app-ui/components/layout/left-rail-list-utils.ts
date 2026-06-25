export const LEFT_RAIL_LIST_PREVIEW_LIMIT = 6;

export function getRecentProjectsForLeftRail<T extends { id: string; updatedAt: number }>(
	projects: T[],
	expandedProjectIds: Set<string>,
	limit: number,
) {
	const normalizedLimit = Math.max(0, limit);
	const sortedProjects = [...projects].sort((a, b) => b.updatedAt - a.updatedAt);

	if (sortedProjects.length <= normalizedLimit) {
		return sortedProjects;
	}

	// 中文注释：最近项目默认只展示固定数量，但已展开的项目必须保留，避免用户展开第二个项目后第一个项目被挤出列表。
	const expandedProjects = sortedProjects.filter((project) => expandedProjectIds.has(project.id));
	const expandedProjectIdSet = new Set(expandedProjects.map((project) => project.id));
	const remainingSlots = Math.max(0, normalizedLimit - expandedProjects.length);
	const fallbackProjects = sortedProjects
		.filter((project) => !expandedProjectIdSet.has(project.id))
		.slice(0, remainingSlots);

	return [...fallbackProjects, ...expandedProjects].sort((a, b) => b.updatedAt - a.updatedAt);
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
