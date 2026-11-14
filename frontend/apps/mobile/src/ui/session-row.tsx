import type { Session } from "@lework/api";
import { router } from "expo-router";
import { Pressable, StyleSheet, Text } from "react-native";

import { sessionRoute } from "../config";
import { formatRelativeTime } from "../sidebar/model";

/**
 * SessionRow 渲染一条会话；标题缺失时退回 id，保证永远可点。
 *
 * 第二行只放节点标签与相对时间：目录与 session id 对用户没有意义，
 * 也会把「节点」重新变成页面主角。
 */
export function SessionRow({
	nodeID,
	session,
	nodeName,
}: {
	nodeID: string;
	session: Session;
	nodeName?: string;
}) {
	const meta = [nodeName ?? "", formatRelativeTime(session.updatedAt)].filter(Boolean).join(" · ");

	return (
		<Pressable style={styles.row} onPress={() => router.push(sessionRoute(nodeID, session.id))}>
			<Text style={styles.title} numberOfLines={1}>
				{session.title || session.id}
			</Text>
			{meta ? (
				<Text style={styles.meta} numberOfLines={1}>
					{meta}
				</Text>
			) : null}
		</Pressable>
	);
}

const styles = StyleSheet.create({
	row: {
		paddingVertical: 10,
		borderBottomWidth: 1,
		borderBottomColor: "#eef2f7",
		gap: 2,
	},
	title: { fontSize: 15, color: "#0f172a" },
	meta: { fontSize: 12, color: "#94a3b8" },
});
