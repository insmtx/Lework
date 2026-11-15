import type { FontSize, Preferences, ProcessDisplay, ToolDisplay } from "./preferences";

const THEME_LABELS = {
	system: "跟随系统",
	light: "亮色",
	dark: "暗色",
} as const;

const FONT_LABELS: Record<FontSize, string> = {
	small: "小字号",
	standard: "标准字号",
	large: "大字号",
};

const PROCESS_LABELS: Record<ProcessDisplay, string> = {
	hidden: "思考隐藏",
	brief: "思考摘要",
	full: "思考完整",
};

const TOOL_LABELS: Record<ToolDisplay, string> = {
	hidden: "工具隐藏",
	collapsed: "工具折叠",
	expanded: "工具展开",
};

/**
 * describeAppearance 生成「界面」菜单行的当前值。
 *
 * 设置首页只列一行，把两个偏好压成一句，用户不用进二级页就知道现在是什么。
 */
export function describeAppearance(preferences: Preferences): string {
	return `${THEME_LABELS[preferences.themeMode]} · ${FONT_LABELS[preferences.fontSize]}`;
}

/** describeProcess 生成「过程展示」菜单行的当前值。 */
export function describeProcess(preferences: Preferences): string {
	return `${PROCESS_LABELS[preferences.processDisplay]} · ${TOOL_LABELS[preferences.toolDisplay]}`;
}

/**
 * describeReminder 生成「提醒」菜单行的当前值。
 *
 * 没连接控制面时推送根本登记不了，此时优先说明连接状态，而不是给出一个
 * 用户无法操作的「推送关闭」。
 */
export function describeReminder(
	preferences: Preferences,
	notifications: boolean,
	connected: boolean,
): string {
	if (!connected) return "未连接";
	if (!notifications) return "推送关闭";
	return preferences.soundEnabled ? "推送开启 · 声音开启" : "推送开启 · 声音关闭";
}

/** describeServer 生成「服务器设置」菜单行的当前值。 */
export function describeServer(baseUrl: string | undefined): string {
	return baseUrl && baseUrl !== "" ? baseUrl : "未连接";
}
