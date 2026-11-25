import type { Session, SessionState } from "@lework/api";
import { router } from "expo-router";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { sessionRoute } from "../config";
import { nodeColorSwatch } from "../nodes/colors";
import { formatRelativeTime } from "../sidebar/model";
import type { Theme } from "../theme/theme";
import { useThemedStyles } from "../theme/use-theme";
import { useAppShell } from "./app-shell-context";
import { RowSweep } from "./row-sweep";
import { UnreadDot } from "./unread-dot";

/**
 * SessionRow 渲染一条会话；标题缺失时退回 id，保证永远可点。
 *
 * 整行底色来自 overview 投影：在跑扫光、等人处理时同一条光带停在行尾、
 * 出错停在行中间呼吸；时间右侧的未读点来自本机水位。两个轴分开，一行就能回答
 * 「在跑吗 / 等我吗 / 有结果没看吗」。节点颜色做成左侧细条：列表里混着多台
 * 电脑时，先按颜色分组，再看状态。
 */
export function SessionRow({
	nodeID,
	session,
	nodeName,
	nodeColor,
	projectName,
	state,
}: {
	nodeID: string;
	session: Session;
	nodeName?: string;
	nodeColor?: string;
	/** 跨项目列表（如首页）传项目名；项目页自身已有上下文，不必传。 */
	projectName?: string;
	state?: SessionState;
}) {
	const styles = useThemedStyles(createStyles);
	const { isUnread } = useAppShell();
	const unread = isUnread(session.updatedAt, nodeID, session.id);
	const meta = [projectName ?? "", nodeName ?? "", formatRelativeTime(session.updatedAt)]
		.filter(Boolean)
		.join(" · ");

	return (
		<Pressable
			style={[styles.row, { borderLeftColor: nodeColorSwatch(nodeColor) ?? "transparent" }]}
			onPress={() => router.push(sessionRoute(nodeID, session.id))}
		>
			<RowSweep state={state} color={nodeColorSwatch(nodeColor)} />
			<View style={styles.titleRow}>
				<Text style={styles.title} numberOfLines={1}>
					{session.title || session.id}
				</Text>
			</View>
			<View style={styles.metaRow}>
				<Text style={styles.meta} numberOfLines={1}>
					{meta}
				</Text>
				{unread ? <UnreadDot /> : null}
			</View>
		</Pressable>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		row: {
			paddingVertical: 10,
			// 始终留出 3px：有颜色时是识别条，没有时也不让文字左右跳。
			borderLeftWidth: 3,
			paddingLeft: 8,
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
			gap: 2,
			// 扫光只铺在行内，不溢到相邻会话。
			overflow: "hidden",
		},
		titleRow: { flexDirection: "row", alignItems: "center", gap: 6 },
		title: { flex: 1, fontSize: 15, color: colors.text },
		metaRow: { flexDirection: "row", alignItems: "center", gap: 6 },
		meta: { flex: 1, fontSize: 12, color: colors.textMuted },
	});
}
