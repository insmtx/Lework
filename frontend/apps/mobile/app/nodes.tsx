import type { NodeView } from "@lework/api";
import { router } from "expo-router";
import { useMemo } from "react";
import { Alert, FlatList, Pressable, RefreshControl, StyleSheet, Text, View } from "react-native";

import { createClient } from "../src/api/client";
import { sortNodes } from "../src/nodes/order";
import { describeRemoveNode, removeErrorMessage, visibleNodes } from "../src/nodes/remove";
import type { Theme } from "../src/theme/theme";
import { useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";
import { LoadingScreen } from "../src/ui/loading";

/**
 * NodeCard 只展示这台电脑本身的状态。
 *
 * 目录只是 agent 上报的可用工作区，展示成文字即可；这里不承担「任务发到
 * 哪」的选择，节点一多才不会混进一堆可点的目录。
 *
 * 移除入口放在卡片右上角的「⋯」：破坏性操作不该一眼可点，但仍要在它对应的
 * 那一行上，用户不必回设置里找。
 */
function NodeCard({ node, onRemove }: { node: NodeView; onRemove: (node: NodeView) => void }) {
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
				<Pressable
					accessibilityRole="button"
					accessibilityLabel={`移除 ${node.name}`}
					onPress={() => onRemove(node)}
					style={styles.moreButton}
				>
					<Text style={styles.moreText}>⋯</Text>
				</Pressable>
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
	const styles = useThemedStyles(createStyles);
	const { settings, settingsLoading, projects } = useAppShell();
	// 不会出现在账号下的历史节点（未认领）不展示：手机不支持认领它们。
	const nodes = useMemo(() => visibleNodes(sortNodes(projects.nodes)), [projects.nodes]);
	const { loading, error, refresh } = projects;
	const onlineCount = nodes.filter((node) => node.connected).length;

	const confirmRemove = (node: NodeView) => {
		Alert.alert(`移除 ${node.name}？`, describeRemoveNode(node.name), [
			{ text: "取消", style: "cancel" },
			{
				text: "移除",
				style: "destructive",
				onPress: () => {
					if (!settings) return;
					void createClient(settings)
						.deleteNode(node.id)
						.then(() => {
							projects.refresh();
						})
						.catch((caught: unknown) => {
							const message = removeErrorMessage(caught);
							if (message) {
								Alert.alert("移除失败", message);
								return;
							}
							// 404 说明别人已经删掉了：结果已达成，照常刷新列表。
							projects.refresh();
						});
				},
			},
		]);
	};

	if (settingsLoading) {
		return <LoadingScreen text="正在加载电脑…" />;
	}

	if (!settings?.sessionToken) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>请先登录</Text>
				<Text style={styles.hint}>节点归属于账号，登录后才能添加或移除电脑。</Text>
				<Pressable style={styles.button} onPress={() => router.push("/login")}>
					<Text style={styles.buttonText}>登录 / 注册</Text>
				</Pressable>
			</View>
		);
	}

	const header = (
		<View style={styles.summary}>
			<View style={styles.summaryRow}>
				<Text style={styles.summaryText}>
					{nodes.length} 台电脑 · {onlineCount} 在线
				</Text>
				<Pressable
					accessibilityRole="button"
					style={styles.addButton}
					onPress={() => router.push("/nodes/add")}
				>
					<Text style={styles.addText}>添加</Text>
				</Pressable>
			</View>
			{error ? <Text style={styles.error}>{error}</Text> : null}
		</View>
	);

	return (
		<FlatList
			contentContainerStyle={styles.list}
			data={nodes}
			keyExtractor={(node) => node.id}
			renderItem={({ item }) => <NodeCard node={item} onRemove={confirmRemove} />}
			refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
			ListHeaderComponent={header}
			ListEmptyComponent={
				<View style={styles.empty}>
					<Text style={styles.title}>还没有电脑</Text>
					<Text style={styles.hint}>
						在电脑上运行 lework agent，终端会显示二维码与 5 位配对码。扫码或输码把它加入这个账号。
					</Text>
					<Pressable style={styles.button} onPress={() => router.push("/nodes/add")}>
						<Text style={styles.buttonText}>扫码添加</Text>
					</Pressable>
					<Pressable
						style={styles.secondaryButton}
						onPress={() => router.push({ pathname: "/nodes/add", params: { mode: "input" } })}
					>
						<Text style={styles.secondaryText}>输入配对码</Text>
					</Pressable>
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
		empty: { alignItems: "center", paddingVertical: 48, paddingHorizontal: 8, gap: 10 },
		title: { fontSize: 18, fontWeight: "600", color: colors.text },
		hint: { color: colors.textSecondary, textAlign: "center", lineHeight: 20 },
		summary: { gap: 4, marginBottom: 4 },
		summaryRow: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
			gap: 8,
		},
		summaryText: { fontSize: 13, color: colors.textSecondary, flexShrink: 1 },
		addButton: {
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingHorizontal: 12,
			paddingVertical: 5,
		},
		addText: { fontSize: 13, fontWeight: "600", color: colors.text },
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
		header: { flexDirection: "row", alignItems: "center", gap: 8 },
		name: { fontSize: 16, fontWeight: "600", flexShrink: 1, color: colors.text },
		status: { fontSize: 12, fontWeight: "600", marginLeft: "auto" },
		online: { color: colors.success },
		offline: { color: colors.offline },
		moreButton: { paddingHorizontal: 8, paddingVertical: 2 },
		moreText: { fontSize: 20, lineHeight: 22, color: colors.textMuted },
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
		secondaryButton: {
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingHorizontal: 16,
			paddingVertical: 10,
		},
		secondaryText: { color: colors.text, fontWeight: "600" },
	});
}
