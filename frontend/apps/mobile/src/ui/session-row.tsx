import type { Session, SessionState } from "@lework/api";
import { router } from "expo-router";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { sessionRoute } from "../config";
import { formatRelativeTime } from "../sidebar/model";
import type { Theme } from "../theme/theme";
import { useThemedStyles } from "../theme/use-theme";
import { useAppShell } from "./app-shell-context";
import { SessionStatusDot, UnreadDot } from "./session-status-dot";

/**
 * SessionRow 渲染一条会话；标题缺失时退回 id，保证永远可点。
 *
 * 标题左侧的状态点来自 overview 投影，时间右侧的未读点来自本机水位；两个
 * 轴分开，一行就能回答「在跑吗 / 等我吗 / 有结果没看吗」。
 */
export function SessionRow({
	nodeID,
	session,
	nodeName,
	state,
}: {
	nodeID: string;
	session: Session;
	nodeName?: string;
	state?: SessionState;
}) {
	const styles = useThemedStyles(createStyles);
	const { isUnread } = useAppShell();
	const unread = isUnread(session.updatedAt, nodeID, session.id);
	const meta = [nodeName ?? "", formatRelativeTime(session.updatedAt)].filter(Boolean).join(" · ");

	return (
		<Pressable
			style={[styles.row, state === "waiting_permission" && styles.waiting]}
			onPress={() => router.push(sessionRoute(nodeID, session.id))}
		>
			<View style={styles.titleRow}>
				<SessionStatusDot state={state} />
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
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
			gap: 2,
		},
		// 等待批准的行轻微高亮：它在抽屉里也被提前，视觉上应当连成一组。
		waiting: {
			backgroundColor: colors.warningSurface,
			borderRadius: 8,
			paddingHorizontal: 8,
		},
		titleRow: { flexDirection: "row", alignItems: "center", gap: 6 },
		title: { flex: 1, fontSize: 15, color: colors.text },
		metaRow: { flexDirection: "row", alignItems: "center", gap: 6 },
		meta: { flex: 1, fontSize: 12, color: colors.textMuted },
	});
}
