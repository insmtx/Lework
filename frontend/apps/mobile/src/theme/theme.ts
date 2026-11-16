import { leworkDark, leworkLight, radii as radiusTokens } from "@lework/design-tokens";

export type ColorScheme = "light" | "dark";

/**
 * ThemeMode 是用户在设置里能选的外观：跟随系统，或强制亮/暗。
 *
 * 与 ColorScheme 分开：mode 是偏好，scheme 是实际生效的配色。
 */
export type ThemeMode = "system" | "light" | "dark";

/**
 * ThemeColors 是手机端的语义色板。
 *
 * 页面只引用语义名，不直接写色值：亮色和暗色各给一套实现，组件不需要知道
 * 自己当前处在哪种主题里，也不会出现「页面亮、代码块暗」的混搭。
 *
 * 亮色取值来自 @lework/design-tokens，与 apps/web 的 --lework-* 保持同源；
 * 暗色由 design-tokens 维护（Web 目前只有亮色壳）。
 */
export interface ThemeColors {
	/** 页面底色，也是导航器的 scene 背景。 */
	background: string;
	/** 卡片、顶栏、弹层等位于页面之上的表面。 */
	surface: string;
	/** 比 surface 略深的输入框/代码行内底色。 */
	surfaceMuted: string;
	/** 再深一层的分隔区域，例如工具卡片的展开区。 */
	surfaceSubtle: string;
	/** 抽屉、侧栏的品牌浅蓝底。 */
	sidebarBackground: string;
	sidebarBorder: string;
	border: string;
	/** 几乎不可见的分隔线，例如列表行之间。 */
	borderSubtle: string;
	/** 需要强调轮廓的边框，例如次级按钮。 */
	borderStrong: string;
	text: string;
	/** 比正文稍弱的标题/正文强调色。 */
	textStrong: string;
	textSecondary: string;
	/** 时间戳、占位符等弱化文字，仍需保持 3:1。 */
	textMuted: string;
	/** 更弱的一层，例如输入框提示、日期。 */
	textSubtle: string;
	/** 链接、选中态、品牌色。 */
	accent: string;
	onAccent: string;
	/** 主按钮底色；与 Web 一致使用品牌靛蓝。 */
	action: string;
	actionText: string;
	/** 选中行、导航激活态的品牌浅底。 */
	primarySoft: string;
	primarySofter: string;
	focusRing: string;
	messageBubble: string;
	userBubble: string;
	userBubbleText: string;
	codeBackground: string;
	codeText: string;
	inlineCodeBackground: string;
	inlineCodeText: string;
	inputBackground: string;
	inputBorder: string;
	placeholder: string;
	danger: string;
	dangerSurface: string;
	warning: string;
	warningSurface: string;
	warningBorder: string;
	success: string;
	offline: string;
	badge: string;
	badgeText: string;
	/** 底部弹层/抽屉后面的遮罩。 */
	modalBackdrop: string;
	/** 抽屉全屏遮罩的底色，配合 Animated 透明度使用。 */
	backdrop: string;
}

export interface Theme {
	scheme: ColorScheme;
	colors: ThemeColors;
	/** 与 Web --lework-radius-* 对齐的圆角尺度。 */
	radii: typeof radiusTokens;
}

const lightColors: ThemeColors = {
	background: leworkLight.appBg,
	surface: leworkLight.surface,
	surfaceMuted: leworkLight.surfaceSoft,
	surfaceSubtle: "#f1f5f9",
	sidebarBackground: leworkLight.sidebarBg,
	sidebarBorder: leworkLight.sidebarBorder,
	border: leworkLight.controlBorder,
	borderSubtle: leworkLight.chatBorder,
	borderStrong: "#bcc8de",
	text: leworkLight.text,
	textStrong: leworkLight.textStrong,
	textSecondary: "#475569",
	textMuted: leworkLight.textMuted,
	textSubtle: leworkLight.textSubtle,
	accent: leworkLight.primary,
	onAccent: "#ffffff",
	action: leworkLight.primary,
	actionText: "#ffffff",
	primarySoft: leworkLight.primarySoft,
	primarySofter: leworkLight.primarySofter,
	focusRing: leworkLight.focusRing,
	messageBubble: leworkLight.chatAiBg,
	userBubble: leworkLight.chatUserBg,
	userBubbleText: leworkLight.chatUserText,
	codeBackground: leworkLight.surfaceSoft,
	codeText: leworkLight.chatText,
	inlineCodeBackground: leworkLight.primarySofter,
	inlineCodeText: leworkLight.text,
	inputBackground: leworkLight.chatInputBg,
	inputBorder: leworkLight.chatInputBorder,
	placeholder: leworkLight.chatPlaceholder,
	danger: "#b91c1c",
	dangerSurface: "#fef2f2",
	warning: "#b45309",
	warningSurface: "#fffbeb",
	warningBorder: "#f59e0b",
	success: "#166534",
	offline: "#6b7280",
	badge: "#ef4444",
	badgeText: "#ffffff",
	modalBackdrop: "rgba(15, 23, 42, 0.28)",
	backdrop: "#0f172a",
};

const darkColors: ThemeColors = {
	background: leworkDark.appBg,
	surface: leworkDark.surface,
	surfaceMuted: leworkDark.surfaceSoft,
	surfaceSubtle: "#1e293b",
	sidebarBackground: leworkDark.sidebarBg,
	sidebarBorder: leworkDark.sidebarBorder,
	border: leworkDark.controlBorder,
	borderSubtle: "#1e293b",
	borderStrong: "#475569",
	text: leworkDark.text,
	textStrong: leworkDark.textStrong,
	textSecondary: "#94a3b8",
	textMuted: leworkDark.textMuted,
	textSubtle: leworkDark.textSubtle,
	accent: leworkDark.primary,
	onAccent: leworkDark.appBg,
	action: leworkDark.primary,
	actionText: leworkDark.appBg,
	primarySoft: leworkDark.primarySoft,
	primarySofter: leworkDark.primarySofter,
	focusRing: leworkDark.focusRing,
	messageBubble: leworkDark.chatAiBg,
	userBubble: leworkDark.chatUserBg,
	userBubbleText: leworkDark.chatUserText,
	codeBackground: "#020617",
	codeText: "#e2e8f0",
	inlineCodeBackground: "#312e81",
	inlineCodeText: "#e0e7ff",
	inputBackground: leworkDark.chatInputBg,
	inputBorder: leworkDark.chatInputBorder,
	placeholder: leworkDark.chatPlaceholder,
	danger: "#f87171",
	dangerSurface: "#3f1d1d",
	warning: "#fbbf24",
	warningSurface: "#3b2a12",
	warningBorder: "#b45309",
	success: "#4ade80",
	offline: "#94a3b8",
	badge: "#ef4444",
	badgeText: "#ffffff",
	modalBackdrop: "rgba(0, 0, 0, 0.55)",
	backdrop: "#000000",
};

export const lightTheme: Theme = {
	scheme: "light",
	colors: lightColors,
	radii: radiusTokens,
};

export const darkTheme: Theme = {
	scheme: "dark",
	colors: darkColors,
	radii: radiusTokens,
};

/** themeFor 把系统配色映射到具体色板；未知值按亮色处理。 */
export function themeFor(scheme: ColorScheme | null | undefined): Theme {
	return scheme === "dark" ? darkTheme : lightTheme;
}

/** resolveScheme 把用户偏好与系统配色合成最终生效的配色。 */
export function resolveScheme(
	mode: ThemeMode,
	systemScheme: ColorScheme | null | undefined,
): ColorScheme {
	if (mode === "light" || mode === "dark") return mode;
	return systemScheme === "dark" ? "dark" : "light";
}
