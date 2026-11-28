import { router } from "expo-router";
import { memo, useCallback, useMemo } from "react";
import { StyleSheet } from "react-native";
import Markdown, { renderRules } from "react-native-markdown-display";

import { fileRoute } from "../config";
import { type ColorScheme, darkTheme, lightTheme, type Theme } from "../theme/theme";
import { useFontScale } from "../theme/use-theme";
import { resolveFileLink } from "./file-links";
import { withFilePathLinks } from "./path-link-rules";
import { withSelectableText } from "./selectable-rules";

// 长按选中复制：正文里的文本节点都来自默认规则，统一补 selectable。
const selectableRules = withSelectableText(renderRules);

/**
 * createMarkdownStyles 把主题色板映射成 Markdown 样式。
 *
 * 代码块不再固定深色：亮色主题用浅灰底 + 深色字，暗色主题才用深色底，
 * 避免整页亮色里突然出现一块黑色区域。字号按 scale 整体缩放，
 * 标题、代码块跟着正文一起变，不会出现正文放大而代码仍很小的割裂感。
 */
function createMarkdownStyles(theme: Theme, scale: number) {
	const { colors } = theme;
	const size = (value: number) => Math.round(value * scale);
	return StyleSheet.create({
		body: { color: colors.text, fontSize: size(15), lineHeight: size(21) },
		paragraph: { marginTop: 0, marginBottom: 6 },
		heading1: {
			color: colors.text,
			fontSize: size(20),
			lineHeight: size(26),
			marginTop: 6,
			marginBottom: 4,
		},
		heading2: {
			color: colors.text,
			fontSize: size(18),
			lineHeight: size(24),
			marginTop: 6,
			marginBottom: 4,
		},
		heading3: {
			color: colors.text,
			fontSize: size(16),
			lineHeight: size(22),
			marginTop: 4,
			marginBottom: 2,
		},
		heading4: {
			color: colors.text,
			fontSize: size(15),
			lineHeight: size(21),
			marginTop: 4,
			marginBottom: 2,
		},
		heading5: {
			color: colors.text,
			fontSize: size(14),
			lineHeight: size(20),
			marginTop: 2,
			marginBottom: 2,
		},
		heading6: {
			color: colors.textSecondary,
			fontSize: size(13),
			lineHeight: size(19),
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
			fontSize: size(13),
			color: colors.inlineCodeText,
			backgroundColor: colors.inlineCodeBackground,
			borderWidth: 0,
			borderRadius: 4,
			paddingHorizontal: 4,
			paddingVertical: 1,
		},
		fence: {
			fontFamily: "monospace",
			fontSize: size(12),
			lineHeight: size(18),
			color: colors.codeText,
			backgroundColor: colors.codeBackground,
			borderWidth: 0,
			borderRadius: 8,
			padding: 10,
			marginVertical: 4,
		},
		code_block: {
			fontFamily: "monospace",
			fontSize: size(12),
			lineHeight: size(18),
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

// 样式按「配色 + 倍率」缓存：字号只有三档，没必要每次渲染都重建样式表。
const styleCache = new Map<string, ReturnType<typeof createMarkdownStyles>>();

function markdownStyles(scheme: ColorScheme, scale: number) {
	const key = `${scheme}:${scale}`;
	const cached = styleCache.get(key);
	if (cached) return cached;
	const theme = scheme === "dark" ? darkTheme : lightTheme;
	const styles = createMarkdownStyles(theme, scale);
	styleCache.set(key, styles);
	return styles;
}

/**
 * MarkdownBody 渲染助手回复的 Markdown。
 *
 * scheme 与全局字号倍率都参与 memo 比较：系统切换亮暗或用户改字号时，
 * 即使消息内容没变也要重新渲染。传了 nodeID 与 directory 时，指向项目
 * 文件的 Markdown 链接和裸路径在应用内打开预览，其余链接仍交给系统浏览器。
 */
export const MarkdownBody = memo(function MarkdownBody({
	content,
	scheme,
	nodeID,
	directory,
}: {
	content: string;
	scheme: ColorScheme;
	nodeID?: string;
	directory?: string;
}) {
	const scale = useFontScale();
	const onLinkPress = useCallback(
		(url: string) => {
			const target = nodeID ? resolveFileLink(url, directory ?? "") : null;
			if (!target || !nodeID) {
				return true;
			}
			router.push(fileRoute(nodeID, target.directory, target.path));
			return false;
		},
		[nodeID, directory],
	);
	// 先包裸路径再套 selectable：含路径的文本不能加 selectable，否则吞掉 onPress。
	const rules = useMemo(() => {
		if (!nodeID) return selectableRules;
		const resolve = (token: string) => {
			const target = resolveFileLink(token, directory ?? "");
			return target ? fileRoute(nodeID, target.directory, target.path) : null;
		};
		return withSelectableText(
			withFilePathLinks(renderRules, resolve, (route) => router.push(route)),
		);
	}, [nodeID, directory]);
	return (
		<Markdown style={markdownStyles(scheme, scale)} rules={rules} onLinkPress={onLinkPress}>
			{content}
		</Markdown>
	);
});
