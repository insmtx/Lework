import type { Session } from "@lework/api";
import { router } from "expo-router";
import { useCallback, useEffect, useState } from "react";
import {
	ActivityIndicator,
	FlatList,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	View,
} from "react-native";

import { createClient } from "../../src/api/client";
import { sessionRoute } from "../../src/config";
import { groupSessions, type NodeSessions } from "../../src/sessions/group";
import { useSettings } from "../../src/settings/use-settings";

function SessionRow({ nodeID, session }: { nodeID: string; session: Session }) {
	return (
		<Pressable style={styles.sessionRow} onPress={() => router.push(sessionRoute(nodeID, session.id))}>
			<Text style={styles.sessionTitle} numberOfLines={1}>
				{session.title || session.id}
			</Text>
			<Text style={styles.sessionMeta} numberOfLines={1}>
				{session.id}
				{session.directory ? ` · ${session.directory}` : ""}
			</Text>
		</Pressable>
	);
}

export default function SessionsScreen() {
	const { settings, loading: settingsLoading } = useSettings();
	const [groups, setGroups] = useState<NodeSessions[]>([]);
	const [loading, setLoading] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const refresh = useCallback(() => {
		if (!settings) return;
		setLoading(true);
		setError(null);
		const client = createClient(settings);
		void client
			.listNodes()
			.then(async (nodes) => {
				const connected = nodes.filter((node) => node.connected);
				// 单个节点拉取失败只影响它自己：其余节点照常显示。
				const entries = await Promise.all(
					connected.map(async (node): Promise<[string, Session[]]> => {
						try {
							return [node.id, await client.listSessions(node.id)];
						} catch {
							return [node.id, []];
						}
					}),
				);
				setGroups(groupSessions(nodes, Object.fromEntries(entries)));
			})
			.catch((caught: unknown) => {
				setError(caught instanceof Error ? caught.message : "failed to load sessions");
			})
			.finally(() => setLoading(false));
	}, [settings]);

	useEffect(() => {
		if (settings) refresh();
	}, [settings, refresh]);

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
				<Text style={styles.hint}>Configure the control plane in Settings first.</Text>
				<Pressable style={styles.button} onPress={() => router.push("/settings")}>
					<Text style={styles.buttonText}>Open settings</Text>
				</Pressable>
			</View>
		);
	}

	return (
		<FlatList
			contentContainerStyle={styles.list}
			data={groups}
			keyExtractor={(group) => group.node.id}
			refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
			ListHeaderComponent={error ? <Text style={styles.error}>{error}</Text> : undefined}
			renderItem={({ item }) => (
				<View style={styles.group}>
					<Text style={styles.nodeName}>{item.node.name}</Text>
					{item.sessions.map((session) => (
						<SessionRow key={session.id} nodeID={item.node.id} session={session} />
					))}
				</View>
			)}
			ListEmptyComponent={
				<View style={styles.center}>
					<Text style={styles.title}>No sessions</Text>
					<Text style={styles.hint}>Sessions on connected nodes will appear here.</Text>
				</View>
			}
		/>
	);
}

const styles = StyleSheet.create({
	list: { padding: 16, gap: 16 },
	center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
	title: { fontSize: 18, fontWeight: "600" },
	hint: { color: "#666", textAlign: "center" },
	error: { color: "#b91c1c", marginBottom: 8 },
	group: { gap: 6 },
	nodeName: { fontSize: 15, fontWeight: "700", color: "#334155" },
	sessionRow: {
		borderWidth: 1,
		borderColor: "#e2e8f0",
		borderRadius: 10,
		padding: 10,
		gap: 2,
	},
	sessionTitle: { fontSize: 14, fontWeight: "500" },
	sessionMeta: { fontSize: 12, color: "#64748b" },
	button: { marginTop: 8, backgroundColor: "#0f172a", borderRadius: 8, paddingHorizontal: 16, paddingVertical: 10 },
	buttonText: { color: "#fff", fontWeight: "600" },
});
