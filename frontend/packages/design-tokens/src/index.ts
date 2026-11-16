/**
 * Design tokens are the single source of truth for the Lework visual language.
 *
 * The web app consumes the same values through `@lework/styles` CSS variables.
 * Keep `leworkLight` in sync with `packages/styles/globals.css`; the parity test
 * in this package fails when the two drift apart.
 */

export const brand = {
	indigo50: "#eef3ff",
	indigo100: "#dce7ff",
	indigo150: "#e8efff",
	indigo500: "#4f46e5",
	blue600: "#0b57d0",
	blue700: "#084fc0",
	focusRing: "#9dbaff",
} as const;

/** Semantic palette shared with the web `--lework-*` variables. */
export const leworkLight = {
	appBg: "#f8fafc",
	sidebarBg: "#eef3ff",
	sidebarBorder: "#c9d3ea",
	surface: "#ffffff",
	surfaceSoft: "#f6f8fc",
	primary: "#4f46e5",
	primaryStrong: "#0b57d0",
	primarySoft: "#dce7ff",
	primarySofter: "#e8efff",
	textStrong: "#111827",
	text: "#263044",
	textMuted: "#6f778a",
	textSubtle: "#8d94a6",
	controlBorder: "#d9dfec",
	focusRing: "#9dbaff",
	chatPageBg: "#ffffff",
	chatPanelBg: "#ffffff",
	chatHeaderBg: "#ffffff",
	chatBorder: "#e3e7f0",
	chatText: "#111827",
	chatTextMuted: "#9aa1b2",
	chatUserBg: "#dfe7ff",
	chatUserText: "#111827",
	chatAiBg: "#fbfcfe",
	chatAiText: "#111827",
	chatAiBorder: "#edf0f5",
	chatDateBg: "#f0f3fb",
	chatDateText: "#b2b7c5",
	chatInputBg: "#ffffff",
	chatInputBorder: "#d8deeb",
	chatInputText: "#111827",
	chatPlaceholder: "#747b8c",
	chatControlBg: "#f1f4fc",
	chatControlText: "#111827",
	chatPrimary: "#0b57d0",
	chatPrimaryHover: "#084fc0",
} as const;

/**
 * Dark palette has no web counterpart yet (the web shell is light-only), so it
 * is owned here to keep the indigo brand hue consistent across platforms.
 */
export const leworkDark = {
	appBg: "#0b1220",
	sidebarBg: "#0f1a30",
	sidebarBorder: "#1e2b4d",
	surface: "#111827",
	surfaceSoft: "#0f172a",
	primary: "#a5b4fc",
	primaryStrong: "#818cf8",
	primarySoft: "#1e2b4d",
	primarySofter: "#18233f",
	textStrong: "#cbd5e1",
	text: "#e2e8f0",
	textMuted: "#7c8ba1",
	textSubtle: "#94a3b8",
	controlBorder: "#334155",
	focusRing: "#6366f1",
	chatPageBg: "#0b1220",
	chatPanelBg: "#111827",
	chatHeaderBg: "#111827",
	chatBorder: "#1e293b",
	chatText: "#e2e8f0",
	chatTextMuted: "#7c8ba1",
	chatUserBg: "#3730a3",
	chatUserText: "#eef2ff",
	chatAiBg: "#1e293b",
	chatAiText: "#e2e8f0",
	chatAiBorder: "#334155",
	chatDateBg: "#1e293b",
	chatDateText: "#94a3b8",
	chatInputBg: "#0f172a",
	chatInputBorder: "#334155",
	chatInputText: "#e2e8f0",
	chatPlaceholder: "#7c8ba1",
	chatControlBg: "#1e293b",
	chatControlText: "#e2e8f0",
	chatPrimary: "#818cf8",
	chatPrimaryHover: "#a5b4fc",
} as const;

export const radii = {
	sm: 8,
	md: 12,
	lg: 16,
	pill: 999,
} as const;

export const shadows = {
	sm: "0 1px 2px rgba(30, 41, 59, 0.05)",
	menu: "0 18px 45px rgba(30, 41, 59, 0.16)",
} as const;

export const typography = {
	fontSans:
		'-apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif',
	fontMono: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
} as const;

export const motion = {
	fast: 150,
	base: 250,
	dropdownOpen: 250,
	dropdownClose: 150,
} as const;

export type LeWorkPalette = typeof leworkLight;
export type RadiusToken = keyof typeof radii;

/**
 * CSS variable names for the shared light palette. Used by the parity test and
 * by documentation; do not rename without updating `packages/styles`.
 */
export const cssVariableByToken: Record<keyof LeWorkPalette, string> = {
	appBg: "--lework-app-bg",
	sidebarBg: "--lework-sidebar-bg",
	sidebarBorder: "--lework-sidebar-border",
	surface: "--lework-surface",
	surfaceSoft: "--lework-surface-soft",
	primary: "--lework-primary",
	primaryStrong: "--lework-primary-strong",
	primarySoft: "--lework-primary-soft",
	primarySofter: "--lework-primary-softer",
	textStrong: "--lework-text-strong",
	text: "--lework-text",
	textMuted: "--lework-text-muted",
	textSubtle: "--lework-text-subtle",
	controlBorder: "--lework-control-border",
	focusRing: "--lework-focus-ring",
	chatPageBg: "--lework-chat-page-bg",
	chatPanelBg: "--lework-chat-panel-bg",
	chatHeaderBg: "--lework-chat-header-bg",
	chatBorder: "--lework-chat-border",
	chatText: "--lework-chat-text",
	chatTextMuted: "--lework-chat-text-muted",
	chatUserBg: "--lework-chat-user-bg",
	chatUserText: "--lework-chat-user-text",
	chatAiBg: "--lework-chat-ai-bg",
	chatAiText: "--lework-chat-ai-text",
	chatAiBorder: "--lework-chat-ai-border",
	chatDateBg: "--lework-chat-date-bg",
	chatDateText: "--lework-chat-date-text",
	chatInputBg: "--lework-chat-input-bg",
	chatInputBorder: "--lework-chat-input-border",
	chatInputText: "--lework-chat-input-text",
	chatPlaceholder: "--lework-chat-placeholder",
	chatControlBg: "--lework-chat-control-bg",
	chatControlText: "--lework-chat-control-text",
	chatPrimary: "--lework-chat-primary",
	chatPrimaryHover: "--lework-chat-primary-hover",
};
