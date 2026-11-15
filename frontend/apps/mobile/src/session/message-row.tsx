import type { Message } from "@lework/api";
import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import type { Theme } from "../theme/theme";
import { useThemePreference } from "../theme/theme-context";
import { useTheme, useThemedStyles } from "../theme/use-theme";
import { ToolCallCard } from "../ui/tool-call/tool-call-card";
import { MarkdownBody } from "./markdown";
import { messageText, rendersMarkdown, toolParts } from "./messages";
import { reasoningText, visibleReasoning } from "./process";

/**
 * ReasoningCard 展示思考过程：brief 先给摘要，点开看完整；full 默认展开。
 *
 * 它和正文分开渲染：思考不是回复内容，塞进气泡会让用户以为模型说过这些话。
 */
export function ReasoningCard({ message, mode }: { message: Message; mode: "brief" | "full" }) {
	const styles = useThemedStyles(createStyles);
	const [expanded, setExpanded] = useState(mode === "full");
	const text = reasoningText(message);
	const preview = visibleReasoning(message, "brief");

	if (!text) return null;

	return (
		<View style={styles.reasoningCard}>
			<Pressable
				style={styles.reasoningHeader}
				onPress={() => setExpanded((open) => !open)}
				accessibilityLabel="展开或收起思考过程"
			>
				<Text style={styles.reasoningChevron}>{expanded ? "▾" : "▸"}</Text>
				<Text style={styles.reasoningLabel}>思考过程</Text>
			</Pressable>
			<Text style={styles.reasoningBody} numberOfLines={expanded ? undefined : 2}>
				{expanded ? text : preview}
			</Text>
		</View>
	);
}

/**
 * MessageRow 渲染一条消息：正文气泡、思考过程、工具调用与错误。
 *
 * 会话页与子代理全屏页共用这一份渲染，避免子代理对话出现另一套样式。
 * nodeID 用于工具卡片的子代理详情与文件预览；sessionID 用于定位该会话
 * 下的子代理子会话。
 */
export function MessageRow({
	message,
	isLastMessage,
	nodeID,
	sessionID,
}: {
	message: Message;
	/** 是否为本会话最后一条消息；最后一张工具卡片据此决定完成后的折叠时机。 */
	isLastMessage: boolean;
	nodeID: string;
	sessionID: string;
}) {
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const { preferences } = useThemePreference();

	// 失败的 idle 是唯一会进入列表的 idle 消息：只留一行错误，不显示状态词。
	if (message.role === "idle") {
		return (
			<View style={styles.noticeRow}>
				<Text style={styles.noticeError}>{message.error?.message ?? "本轮执行失败"}</Text>
			</View>
		);
	}

	const isUser = message.role === "user";
	const text = messageText(message);
	const tools = preferences.toolDisplay === "hidden" ? [] : toolParts(message);
	// 助手回复可能是 Markdown；用户输入保持纯文本。
	const markdown = rendersMarkdown(message) && text !== "";
	const reasoning =
		!isUser && preferences.processDisplay !== "hidden" && reasoningText(message) !== "";

	return (
		<View style={[styles.turn, isUser ? styles.userTurn : styles.assistantTurn]}>
			{text ? (
				<View style={[styles.bubble, isUser ? styles.userBubble : styles.assistantBubble]}>
					{markdown ? (
						<MarkdownBody content={text} scheme={theme.scheme} />
					) : (
						<Text style={[styles.messageText, isUser && styles.userMessageText]}>{text}</Text>
					)}
				</View>
			) : null}
			{reasoning ? (
				<ReasoningCard
					message={message}
					mode={preferences.processDisplay === "full" ? "full" : "brief"}
				/>
			) : null}
			{!isUser && tools.length > 0 ? (
				<View style={styles.tools}>
					{tools.map((part, index) => (
						<ToolCallCard
							key={part.callID || `${part.tool || "tool"}-${index}`}
							part={part}
							defaultExpanded={preferences.toolDisplay === "expanded"}
							isLast={isLastMessage && index === tools.length - 1}
							nodeID={nodeID}
							sessionID={sessionID}
						/>
					))}
				</View>
			) : null}
			{message.error ? <Text style={styles.turnError}>{message.error.message}</Text> : null}
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		turn: { gap: 6 },
		userTurn: { alignItems: "flex-end" },
		assistantTurn: { alignItems: "flex-start" },
		bubble: { maxWidth: "82%", borderRadius: 18, paddingHorizontal: 14, paddingVertical: 10 },
		userBubble: { backgroundColor: colors.userBubble, borderBottomRightRadius: 4 },
		assistantBubble: { backgroundColor: colors.messageBubble, borderTopLeftRadius: 4 },
		messageText: { fontSize: 15, lineHeight: 21, color: colors.text },
		userMessageText: { color: colors.userBubbleText },
		reasoningCard: {
			alignSelf: "flex-start",
			maxWidth: "88%",
			backgroundColor: colors.surfaceMuted,
			borderRadius: 12,
			padding: 10,
			gap: 4,
		},
		reasoningHeader: { flexDirection: "row", alignItems: "center", gap: 6 },
		reasoningChevron: { width: 12, fontSize: 12, color: colors.textMuted },
		reasoningLabel: { fontSize: 12, fontWeight: "600", color: colors.textSecondary },
		reasoningBody: { fontSize: 12, lineHeight: 18, color: colors.textSecondary },
		tools: { width: "100%", gap: 6 },
		noticeRow: { alignItems: "flex-start", paddingHorizontal: 4 },
		noticeError: { fontSize: 12, color: colors.danger },
		turnError: { maxWidth: "88%", paddingHorizontal: 4, fontSize: 12, color: colors.danger },
	});
}
