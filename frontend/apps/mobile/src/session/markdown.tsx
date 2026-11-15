import { memo } from "react";
import { StyleSheet } from "react-native";
import Markdown from "react-native-markdown-display";

import { type ColorScheme, darkTheme, lightTheme, type Theme } from "../theme/theme";

/**
 * createMarkdownStyles 把主题色板映射成 Markdown 样式。
 *
 * 代码块不再固定深色：亮色主题用浅灰底 + 深色字，暗色主题才用深色底，
 * 避免整页亮色里突然出现一块黑色区域。
 */
function createMarkdownStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		body: { color: colors.text, fontSize: 15, lineHeight: 21 },
		paragraph: { marginTop: 0, marginBottom: 6 },
		heading1: {
			color: colors.text,
			fontSize: 20,
			lineHeight: 26,
			marginTop: 6,
			marginBottom: 4,
		},
		heading2: {
			color: colors.text,
			fontSize: 18,
			lineHeight: 24,
			marginTop: 6,
			marginBottom: 4,
		},
		heading3: {
			color: colors.text,
			fontSize: 16,
			lineHeight: 22,
			marginTop: 4,
			marginBottom: 2,
		},
		heading4: {
			color: colors.text,
			fontSize: 15,
			lineHeight: 21,
			marginTop: 4,
			marginBottom: 2,
		},
		heading5: {
			color: colors.text,
			fontSize: 14,
			lineHeight: 20,
			marginTop: 2,
			marginBottom: 2,
		},
		heading6: {
			color: colors.textSecondary,
			fontSize: 13,
			lineHeight: 19,
			marginTop: 2,
			marginBottom: 2,
		},
		strong: { fontWeight: "700" },
		em: { fontStyle: "italic" },
		link: { color: colors.accent, textDecorationLine: "underline" },
		blockquote: {
			backgroundColor: colors.surfaceMuted,
			borderLeftWidth: 3,
			borderColor: colors.borderStrong,
			borderRadius: 6,
			marginLeft: 0,
			paddingHorizontal: 10,
			paddingVertical: 6,
		},
		hr: { backgroundColor: colors.border, height: 1, marginVertical: 8 },
		code_inline: {
			fontFamily: "monospace",
			fontSize: 13,
			color: colors.inlineCodeText,
			backgroundColor: colors.inlineCodeBackground,
			borderWidth: 0,
			borderRadius: 4,
			paddingHorizontal: 4,
			paddingVertical: 1,
		},
		fence: {
			fontFamily: "monospace",
			fontSize: 12,
			lineHeight: 18,
			color: colors.codeText,
			backgroundColor: colors.codeBackground,
			borderWidth: 0,
			borderRadius: 8,
			padding: 10,
			marginVertical: 4,
		},
		code_block: {
			fontFamily: "monospace",
			fontSize: 12,
			lineHeight: 18,
			color: colors.codeText,
			backgroundColor: colors.codeBackground,
			borderWidth: 0,
			borderRadius: 8,
			padding: 10,
			marginVertical: 4,
		},
		bullet_list_icon: { marginLeft: 2, marginRight: 6, color: colors.textSecondary },
		ordered_list_icon: { marginLeft: 2, marginRight: 6, color: colors.textSecondary },
		list_item: { marginBottom: 2 },
		table: { borderWidth: 1, borderColor: colors.border, borderRadius: 6 },
		th: { flex: 1, padding: 6, backgroundColor: colors.surfaceMuted },
		td: { flex: 1, padding: 6 },
		tr: { borderBottomWidth: 1, borderColor: colors.border, flexDirection: "row" },
	});
}

const stylesByScheme = {
	light: createMarkdownStyles(lightTheme),
	dark: createMarkdownStyles(darkTheme),
} satisfies Record<ColorScheme, unknown>;

/**
 * MarkdownBody 渲染助手回复的 Markdown。
 *
 * scheme 作为 prop 参与 memo 比较：系统切换亮暗时，即使消息内容没变也要
 * 重新渲染一套颜色。
 */
export const MarkdownBody = memo(function MarkdownBody({
	content,
	scheme,
}: {
	content: string;
	scheme: ColorScheme;
}) {
	return <Markdown style={stylesByScheme[scheme]}>{content}</Markdown>;
});
