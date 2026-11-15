import type { NodeView } from "@lework/api";
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

import type { Theme } from "../src/theme/theme";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";

function NodeCard({ node }: { node: NodeView }) {
	const styles = useThemedStyles(createStyles);
	const lastSeen = node.last_seen ? new Date(node.last_seen).toLocaleString() : "never";
	const roots = node.workspace_roots ?? [];

	return (
		<View style={[styles.card, !node.connected && styles.cardOffline]}>
			<View style={styles.header}>
				<Text style={styles.name} numberOfLines={1}>
					{node.name}
				</Text>
				<Text style={[styles.status, node.connected ? styles.online : styles.offline]}>
					{node.connected ? "online" : "offline"}
				</Text>
			</View>
			<Text style={styles.meta}>
				{node.os}/{node.arch} · agent {node.agent_version}
			</Text>
			<Text style={styles.meta}>last seen {lastSeen}</Text>
			{roots.length > 0 ? (
				<View style={styles.roots}>
					<Text style={styles.rootsLabel}>工作目录</Text>
					{roots.map((root) => (
						<Text key={root} style={styles.root} numberOfLines={1}>
							{root}
						</Text>
					))}
				</View>
			) : null}
		</View>
	);
}

/** 节点页只做诊断：节点不再进主导航，只从侧边栏设置/诊断入口进入。 */
export default function NodesScreen() {
	useScreenTitle("节点");
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const { settings, settingsLoading, projects } = useAppShell();
	const nodes = projects.nodes;
	const loading = projects.loading;
	const error = projects.error;
	const refresh = projects.refresh;

	if (settingsLoading) {
		return (
			<View style={styles.center}>
				<ActivityIndicator color={theme.colors.accent} />
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
			data={nodes}
			keyExtractor={(node) => node.id}
			renderItem={({ item }) => <NodeCard node={item} />}
			refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
			ListHeaderComponent={error ? <Text style={styles.error}>{error}</Text> : undefined}
			ListEmptyComponent={
				<View style={styles.center}>
					<Text style={styles.title}>No nodes</Text>
					<Text style={styles.hint}>Start an agent and it will register here.</Text>
				</View>
			}
		/>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		list: { padding: 16, gap: 12 },
		center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
		title: { fontSize: 18, fontWeight: "600", color: colors.text },
		hint: { color: colors.textSecondary, textAlign: "center" },
		error: { color: colors.danger, marginBottom: 8 },
		card: {
			borderWidth: 1,
			borderColor: colors.border,
			backgroundColor: colors.surface,
			borderRadius: 12,
			padding: 12,
			gap: 4,
		},
		cardOffline: { opacity: 0.7 },
		header: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 },
		name: { fontSize: 16, fontWeight: "600", flexShrink: 1, color: colors.text },
		status: { fontSize: 12, fontWeight: "600" },
		online: { color: colors.success },
		offline: { color: colors.offline },
		meta: { fontSize: 12, color: colors.textSecondary },
		roots: { marginTop: 4, gap: 2 },
		rootsLabel: { fontSize: 12, fontWeight: "600", color: colors.textStrong },
		root: { fontSize: 12, color: colors.textSecondary },
		button: {
			marginTop: 8,
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingHorizontal: 16,
			paddingVertical: 10,
		},
		buttonText: { color: colors.actionText, fontWeight: "600" },
	});
}
