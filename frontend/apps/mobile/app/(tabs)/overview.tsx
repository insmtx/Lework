import type { OverviewNode, OverviewSession, SessionState } from "@lework/api";
import { router } from "expo-router";
import {
	ActivityIndicator,
	FlatList,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	View,
} from "react-native";

import { sessionRoute } from "../../src/config";
import { orderNodes } from "../../src/overview/order";
import { useOverview } from "../../src/overview/use-overview";
import { useSettings } from "../../src/settings/use-settings";

const STATE_LABEL: Record<SessionState, string> = {
	running: "Running",
	waiting_permission: "Needs approval",
	idle: "Idle",
	error: "Error",
	offline: "Offline",
};

const STATE_COLOR: Record<SessionState, string> = {
	running: "#166534",
	waiting_permission: "#b45309",
	idle: "#475569",
	error: "#b91c1c",
	offline: "#6b7280",
};

function SessionRow({ nodeID, session }: { nodeID: string; session: OverviewSession }) {
	return (
		<Pressable
			style={styles.sessionRow}
			onPress={() => router.push(sessionRoute(nodeID, session.session_id))}
		>
			<View style={styles.sessionText}>
				<Text style={styles.sessionTitle} numberOfLines={1}>
					{session.title || session.session_id}
				</Text>
				<Text style={styles.sessionMeta} numberOfLines={1}>
					{session.session_id}
					{session.pending_permissions > 0 ? ` · ${session.pending_permissions} pending` : ""}
				</Text>
			</View>
			<Text style={[styles.state, { color: STATE_COLOR[session.state] ?? "#6b7280" }]}>
				{STATE_LABEL[session.state] ?? session.state}
			</Text>
		</Pressable>
	);
}

function NodeCard({ node }: { node: OverviewNode }) {
	return (
		<View style={[styles.card, !node.connected && styles.cardOffline]}>
			<View style={styles.nodeHeader}>
				<Text style={styles.nodeName} numberOfLines={1}>
					{node.name}
				</Text>
				<Text style={[styles.nodeStatus, node.connected ? styles.online : styles.offline]}>
					{node.connected ? "online" : "offline"}
				</Text>
			</View>
			{node.sessions.length === 0 ? (
				<Text style={styles.emptyHint}>No sessions</Text>
			) : (
				node.sessions.map((session) => (
					<SessionRow key={session.session_id} nodeID={node.node_id} session={session} />
				))
			)}
		</View>
	);
}

export default function OverviewScreen() {
	const { settings, loading: settingsLoading } = useSettings();
	const { nodes, loading, error, refresh, connected } = useOverview(settings);

	if (settingsLoading) {
		return (
			<View style={styles.center}>
				<ActivityIndicator />
			</View>
		);
	}

	if (!settings) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>Not connected</Text>
				<Text style={styles.hint}>Add your control plane address and token to see all nodes.</Text>
				<Pressable style={styles.button} onPress={() => router.push("/settings")}>
					<Text style={styles.buttonText}>Open settings</Text>
				</Pressable>
			</View>
		);
	}

	return (
		<FlatList
			style={styles.list}
			contentContainerStyle={styles.listContent}
			data={orderNodes(nodes)}
			keyExtractor={(node) => node.node_id}
			renderItem={({ item }) => <NodeCard node={item} />}
			refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
			ListHeaderComponent={
				<View style={styles.header}>
					{error ? <Text style={styles.error}>{error}</Text> : null}
					{!connected ? <Text style={styles.hint}>Live updates offline — pull to refresh.</Text> : null}
				</View>
			}
			ListEmptyComponent={
				<View style={styles.center}>
					<Text style={styles.title}>Nothing running</Text>
					<Text style={styles.hint}>Sessions started on any node will show up here.</Text>
				</View>
			}
		/>
	);
}

const styles = StyleSheet.create({
	list: { flex: 1 },
	listContent: { padding: 16, gap: 12 },
	header: { gap: 6 },
	center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
	title: { fontSize: 18, fontWeight: "600" },
	hint: { color: "#666", textAlign: "center" },
	error: { color: "#b91c1c" },
	card: {
		borderWidth: 1,
		borderColor: "#e2e8f0",
		borderRadius: 12,
		padding: 12,
		gap: 6,
	},
	cardOffline: { opacity: 0.7 },
	nodeHeader: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 },
	nodeName: { fontSize: 16, fontWeight: "600", flexShrink: 1 },
	nodeStatus: { fontSize: 12, fontWeight: "600" },
	online: { color: "#166534" },
	offline: { color: "#6b7280" },
	emptyHint: { color: "#94a3b8", fontSize: 13 },
	sessionRow: {
		flexDirection: "row",
		alignItems: "center",
		gap: 8,
		paddingVertical: 8,
		borderTopWidth: 1,
		borderTopColor: "#f1f5f9",
	},
	sessionText: { flex: 1, gap: 2 },
	sessionTitle: { fontSize: 14, fontWeight: "500" },
	sessionMeta: { fontSize: 12, color: "#64748b" },
	state: { fontSize: 12, fontWeight: "600" },
	button: { marginTop: 8, backgroundColor: "#0f172a", borderRadius: 8, paddingHorizontal: 16, paddingVertical: 10 },
	buttonText: { color: "#fff", fontWeight: "600" },
});
