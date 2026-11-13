import type { Session } from "@lework/api";
import { router } from "expo-router";
import { Pressable, StyleSheet, Text } from "react-native";

import { sessionRoute } from "../config";

/** SessionRow 渲染一条会话；标题缺失时退回 id，保证永远可点。 */
export function SessionRow({
	nodeID,
	session,
	nodeName,
}: {
	nodeID: string;
	session: Session;
	nodeName?: string;
}) {
	const meta = [session.id, nodeName ?? nodeID, session.subpath || session.directory]
		.filter(Boolean)
		.join(" · ");

	return (
		<Pressable style={styles.row} onPress={() => router.push(sessionRoute(nodeID, session.id))}>
			<Text style={styles.title} numberOfLines={1}>
				{session.title || session.id}
			</Text>
			<Text style={styles.meta} numberOfLines={1}>
				{meta}
			</Text>
		</Pressable>
	);
}

const styles = StyleSheet.create({
	row: {
		borderWidth: 1,
		borderColor: "#e2e8f0",
		borderRadius: 10,
		padding: 10,
		gap: 2,
	},
	title: { fontSize: 15, fontWeight: "500" },
	meta: { fontSize: 12, color: "#64748b" },
});
