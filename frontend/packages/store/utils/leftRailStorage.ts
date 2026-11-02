export const LEFT_RAIL_WIDTH_STORAGE_KEY = "lework-left-rail-width";
export const LEFT_RAIL_COLLAPSED_STORAGE_KEY = "lework-left-rail-collapsed";

// 左侧栏可拖动宽度的上下限与默认值（px）
export const LEFT_RAIL_MIN_WIDTH = 236;
export const LEFT_RAIL_MAX_WIDTH = 320;
export const LEFT_RAIL_DEFAULT_WIDTH = 240;

export type StoredLeftRailPreferences = {
	width: number;
	collapsed: boolean;
};

/** 将侧边栏宽度限制在可读且不挤压主内容的范围内。 */
export function clampLeftRailWidth(
	width: number,
	minWidth = LEFT_RAIL_MIN_WIDTH,
	maxWidth = LEFT_RAIL_MAX_WIDTH,
): number {
	return Math.min(maxWidth, Math.max(minWidth, Math.round(width)));
}

/** 首屏渲染前同步读取侧边栏偏好，避免刷新后宽度从默认值跳变。 */
export function readStoredLeftRailPreferences(): StoredLeftRailPreferences {
	if (typeof window === "undefined") {
		return { width: LEFT_RAIL_DEFAULT_WIDTH, collapsed: false };
	}

	let width = LEFT_RAIL_DEFAULT_WIDTH;
	let collapsed = false;

	try {
		const savedWidth = window.localStorage.getItem(LEFT_RAIL_WIDTH_STORAGE_KEY);
		if (savedWidth) {
			const parsedWidth = Number(savedWidth);
			if (Number.isFinite(parsedWidth)) {
				width = clampLeftRailWidth(parsedWidth);
			}
		}

		const savedCollapsed = window.localStorage.getItem(LEFT_RAIL_COLLAPSED_STORAGE_KEY);
		if (savedCollapsed) {
			collapsed = savedCollapsed === "true";
		}
	} catch (err) {
		console.error("read left rail preferences error:", err);
	}

	return { width, collapsed };
}

export function writeStoredLeftRailWidth(width: number) {
	if (typeof window === "undefined") return;

	try {
		window.localStorage.setItem(LEFT_RAIL_WIDTH_STORAGE_KEY, String(width));
	} catch (err) {
		console.error("save left rail width error:", err);
	}
}

export function writeStoredLeftRailCollapsed(collapsed: boolean) {
	if (typeof window === "undefined") return;

	try {
		window.localStorage.setItem(LEFT_RAIL_COLLAPSED_STORAGE_KEY, String(collapsed));
	} catch (err) {
		console.error("save left rail collapsed error:", err);
	}
}
