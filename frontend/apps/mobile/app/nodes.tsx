import type { NodeView } from "@lework/api";
import { router } from "expo-router";
import { useMemo } from "react";
import {
	ActivityIndicator,
	FlatList,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	View,
} from "react-native";

import { sortNodes } from "../src/nodes/order";
import type { Theme } from "../src/theme/theme";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";

/**
 * NodeCard 只展示这台电脑本身的状态。
 *
 * 目录只是 agent 上报的可用工作区，展示成文字即可；这里不承担「任务发到
 * 哪」的选择，节点一多才不会混进一堆可点的目录。
 */
function NodeCard({ node }: { node: NodeView }) {
	const styles = useThemedStyles(createStyles);
	const lastSeen = node.last_seen ? new Date(node.last_seen).toLocaleString() : "从未";
	const roots = node.workspace_roots ?? [];

	return (
		<View style={[styles.card, !node.connected && styles.cardOffline]}>
			<View style={styles.header}>
				<Text style={styles.name} numberOfLines={1}>
					{node.name}
				</Text>
				<Text style={[styles.status, node.connected ? styles.online : styles.offline]}>
					{node.connected ? "在线" : "离线"}
				</Text>
			</View>
			<Text style={styles.meta}>
				{node.os}/{node.arch} · agent {node.agent_version}
			</Text>
			<Text style={styles.meta}>最后在线 {lastSeen}</Text>

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

/** 我的电脑只做节点管理：在线优先排序，最多给一行在线统计。 */
export default function NodesScreen() {
	useScreenTitle("我的电脑");
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const { settings, settingsLoading, projects } = useAppShell();
	const nodes = useMemo(() => sortNodes(projects.nodes), [projects.nodes]);
	const { loading, error, refresh } = projects;
	const onlineCount = nodes.filter((node) => node.connected).length;

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
				<Text style={styles.title}>未连接</Text>
				<Text style={styles.hint}>先在设置里填写控制面地址与令牌。</Text>
				<Pressable style={styles.button} onPress={() => router.push("/settings")}>
					<Text style={styles.buttonText}>打开设置</Text>
				</Pressable>
			</View>
		);
	}

	const header =
		nodes.length > 0 || error ? (
			<View style={styles.summary}>
				{nodes.length > 0 ? (
					<Text style={styles.summaryText}>
						{nodes.length} 台电脑 · {onlineCount} 在线
					</Text>
				) : null}
				{error ? <Text style={styles.error}>{error}</Text> : null}
			</View>
		) : undefined;

	return (
		<FlatList
			contentContainerStyle={styles.list}
			data={nodes}
			keyExtractor={(node) => node.id}
			renderItem={({ item }) => <NodeCard node={item} />}
			refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
			ListHeaderComponent={header}
			ListEmptyComponent={
				<View style={styles.center}>
					<Text style={styles.title}>还没有电脑</Text>
					<Text style={styles.hint}>在电脑上启动 lework agent 并接入控制面后，会出现在这里。</Text>
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
		summary: { gap: 4, marginBottom: 4 },
		summaryText: { fontSize: 13, color: colors.textSecondary },
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
		roots: { marginTop: 8, gap: 2 },
		rootsLabel: { fontSize: 13, fontWeight: "600", color: colors.textStrong },
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
