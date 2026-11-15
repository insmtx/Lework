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
	/** 链接、选中态、品牌色。 */
	accent: string;
	onAccent: string;
	/** 主按钮底色；亮色主题是近黑，暗色主题用蓝色。 */
	action: string;
	actionText: string;
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
}

export const lightTheme: Theme = {
	scheme: "light",
	colors: {
		background: "#ffffff",
		surface: "#ffffff",
		surfaceMuted: "#f8fafc",
		surfaceSubtle: "#f1f5f9",
		border: "#e2e8f0",
		borderSubtle: "#eef2f7",
		borderStrong: "#cbd5e1",
		text: "#0f172a",
		textStrong: "#334155",
		textSecondary: "#475569",
		textMuted: "#6b7a90",
		accent: "#2563eb",
		onAccent: "#ffffff",
		action: "#0f172a",
		actionText: "#ffffff",
		messageBubble: "#f1f5f9",
		userBubble: "#2563eb",
		userBubbleText: "#ffffff",
		codeBackground: "#f1f5f9",
		codeText: "#0f172a",
		inlineCodeBackground: "#e2e8f0",
		inlineCodeText: "#0f172a",
		inputBackground: "#f8fafc",
		inputBorder: "#e2e8f0",
		placeholder: "#6b7a90",
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
	},
};

export const darkTheme: Theme = {
	scheme: "dark",
	colors: {
		background: "#0b1220",
		surface: "#111827",
		surfaceMuted: "#0f172a",
		surfaceSubtle: "#1e293b",
		border: "#334155",
		borderSubtle: "#1e293b",
		borderStrong: "#475569",
		text: "#e2e8f0",
		textStrong: "#cbd5e1",
		textSecondary: "#94a3b8",
		textMuted: "#7c8ba1",
		accent: "#60a5fa",
		onAccent: "#0b1220",
		action: "#2563eb",
		actionText: "#ffffff",
		messageBubble: "#1e293b",
		userBubble: "#2563eb",
		userBubbleText: "#ffffff",
		codeBackground: "#020617",
		codeText: "#e2e8f0",
		inlineCodeBackground: "#334155",
		inlineCodeText: "#e2e8f0",
		inputBackground: "#0f172a",
		inputBorder: "#334155",
		placeholder: "#7c8ba1",
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
	},
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
