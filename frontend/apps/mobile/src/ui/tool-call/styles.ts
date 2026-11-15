import { StyleSheet } from "react-native";

import type { Theme } from "../../theme/theme";

/**
 * createStyles 汇总工具卡片的样式。
 *
 * 所有子组件共用同一个工厂，样式表只随主题与字号重建一次；卡片内部不再
 * 各自维护一份几乎相同的字段/代码/差异排版。fontScale 来自会话字号设置，
 * 与正文使用同一个倍率，避免正文变大而工具输出仍停留在小字。
 */
export function createStyles(theme: Theme, fontScale = 1) {
	const { colors } = theme;
	const size = (value: number) => Math.round(value * fontScale);

	return StyleSheet.create({
		card: {
			alignSelf: "flex-start",
			maxWidth: "88%",
			borderWidth: 1,
			borderColor: colors.border,
			backgroundColor: colors.surface,
			borderRadius: 12,
			overflow: "hidden",
		},
		/** 运行中强调边框，让进行中的调用在消息流里一眼可见。 */
		cardRunning: { borderColor: colors.accent },
		header: {
			flexDirection: "row",
			alignItems: "center",
			gap: 6,
			paddingHorizontal: 10,
			paddingVertical: 8,
		},
		chevron: { width: size(12), fontSize: size(12), color: colors.textMuted },
		toolName: {
			flexShrink: 1,
			fontFamily: "monospace",
			fontSize: size(13),
			fontWeight: "600",
			color: colors.text,
		},
		status: { marginLeft: "auto", fontSize: size(11) },
		statusRunning: { fontWeight: "600" },
		spinner: { marginLeft: 6 },
		summary: {
			paddingHorizontal: 10,
			paddingBottom: 8,
			marginTop: -2,
			fontSize: size(12),
			color: colors.textSecondary,
		},
		body: {
			borderTopWidth: 1,
			borderTopColor: colors.borderSubtle,
			padding: 10,
			gap: 8,
		},
		blocks: { gap: 8 },
		block: { gap: 4 },
		label: {
			fontSize: size(10),
			fontWeight: "700",
			color: colors.textMuted,
			textTransform: "uppercase",
		},
		terminal: {
			backgroundColor: colors.codeBackground,
			borderRadius: 8,
			padding: 8,
			gap: 2,
		},
		command: {
			fontFamily: "monospace",
			fontSize: size(12),
			lineHeight: size(18),
			fontWeight: "600",
			color: colors.codeText,
		},
		workdir: {
			fontFamily: "monospace",
			fontSize: size(11),
			lineHeight: size(16),
			color: colors.textMuted,
		},
		terminalOutput: {
			fontFamily: "monospace",
			fontSize: size(12),
			lineHeight: size(18),
			color: colors.codeText,
			marginTop: 4,
		},
		terminalMeta: { marginTop: 4, fontSize: size(10), color: colors.textMuted },
		code: {
			fontFamily: "monospace",
			fontSize: size(12),
			lineHeight: size(18),
			color: colors.codeText,
			backgroundColor: colors.codeBackground,
			borderRadius: 8,
			padding: 8,
		},
		diff: {
			backgroundColor: colors.codeBackground,
			borderRadius: 8,
			padding: 8,
		},
		diffLine: {
			fontFamily: "monospace",
			fontSize: size(12),
			lineHeight: size(17),
			color: colors.codeText,
		},
		diffAdd: { color: colors.success, backgroundColor: colors.surfaceMuted },
		diffDel: { color: colors.danger, backgroundColor: colors.dangerSurface },
		diffMeta: { color: colors.textMuted },
		muted: { fontSize: size(12), lineHeight: size(18), color: colors.textMuted },
		empty: {
			fontSize: size(12),
			color: colors.textMuted,
			backgroundColor: colors.codeBackground,
			borderRadius: 8,
			padding: 8,
		},
		listItem: {
			fontSize: size(12),
			lineHeight: size(18),
			color: colors.textStrong,
			fontFamily: "monospace",
		},
		checkItem: { fontSize: size(12), lineHeight: size(18), color: colors.textStrong },
		checkDone: { color: colors.textMuted, textDecorationLine: "line-through" },
		fields: { gap: 6 },
		fieldRow: { gap: 2 },
		fieldLabel: {
			fontSize: size(10),
			fontWeight: "700",
			color: colors.textMuted,
			textTransform: "uppercase",
		},
		fieldValue: {
			fontFamily: "monospace",
			fontSize: size(12),
			lineHeight: size(18),
			color: colors.textStrong,
		},
		text: { fontSize: size(13), lineHeight: size(19), color: colors.text },
		textMuted: { color: colors.textMuted },
		textError: { color: colors.danger },
		errorText: { color: colors.danger },
		raw: { gap: 4 },
		rawToggle: { fontSize: size(11), color: colors.textMuted },
		rawBody: {
			fontFamily: "monospace",
			fontSize: size(11),
			lineHeight: size(16),
			color: colors.textSecondary,
			backgroundColor: colors.surfaceMuted,
			borderRadius: 8,
			padding: 8,
		},
		sheetScreen: { flex: 1, backgroundColor: colors.background },
		sheetHeader: {
			flexDirection: "row",
			alignItems: "center",
			gap: 12,
			paddingHorizontal: 16,
			paddingVertical: 12,
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
		},
		sheetHeading: { flex: 1, gap: 2 },
		sheetTitle: { fontSize: size(16), fontWeight: "700", color: colors.text },
		sheetSubtitle: { fontSize: size(12), color: colors.textSecondary },
		sheetClose: { fontSize: size(14), fontWeight: "600", color: colors.accent },
		sheetScroll: { flex: 1 },
		sheetContent: { padding: 16, gap: 12 },
		sheetMeta: { gap: 4 },
		sheetMetaText: { fontSize: size(12), color: colors.textMuted },
		sheetDescription: { fontSize: size(14), lineHeight: size(20), color: colors.text },
		primaryButton: {
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingHorizontal: 14,
			paddingVertical: 10,
			alignItems: "center",
			justifyContent: "center",
		},
		primaryButtonText: { color: colors.actionText, fontWeight: "600" },
	});
}
