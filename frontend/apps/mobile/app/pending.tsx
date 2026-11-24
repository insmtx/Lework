import type { PendingForm } from "@lework/api";
import { router } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import { FlatList, Pressable, RefreshControl, StyleSheet, Text, View } from "react-native";

import { createClient } from "../src/api/client";
import { sessionRoute } from "../src/config";
import { nodeColorSwatch } from "../src/nodes/colors";
import { type PendingRow, pendingRows } from "../src/pending/derive";
import type { Theme } from "../src/theme/theme";
import { useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";
import { LoadingScreen } from "../src/ui/loading";

interface GlobalItem {
	nodeID: string;
	nodeName: string;
	form: PendingForm;
}

type Item =
	| { kind: "session"; row: PendingRow }
	| { kind: "global-header" }
	| { kind: "global"; item: GlobalItem };

/** 待处理：会话里的审批/表单，加上各在线节点的位置级表单。 */
export default function PendingScreen() {
	useScreenTitle("待处理");
	const styles = useThemedStyles(createStyles);
	const {
		settings,
		settingsLoading,
		projects,
		overviewNodes: nodes,
		overviewLoading: loading,
		overviewError: error,
		refreshOverview: refresh,
	} = useAppShell();
	const nodeByID = useMemo(
		() => Object.fromEntries(projects.nodes.map((node) => [node.id, node])),
		[projects.nodes],
	);

	const rows = useMemo(
		() =>
			pendingRows(
				nodes.map((node) => ({
					node: { id: node.node_id, connected: node.connected },
					sessions: node.sessions,
				})),
			),
		[nodes],
	);

	// global 表单没有会话行，只能逐节点查询；按在线节点集合的变化重拉，
	// 避免每条总览增量事件都打一轮表单接口。
	const onlineKey = useMemo(
		() =>
			nodes
				.filter((node) => node.connected)
				.map((node) => `${node.node_id}:${node.name}`)
				.join("|"),
		[nodes],
	);

	const [globalItems, setGlobalItems] = useState<GlobalItem[]>([]);
	const [formsLoading, setFormsLoading] = useState(false);
	const [formsRefresh, setFormsRefresh] = useState(0);

	// biome-ignore lint/correctness/useExhaustiveDependencies: 只按在线节点集合与手动刷新重拉表单，避免总览增量事件触发重复请求
	useEffect(() => {
		if (!settings) return;
		const client = createClient(settings);
		const targets = nodes
			.filter((node) => node.connected)
			.map((node) => ({ id: node.node_id, name: node.name }));
		let cancelled = false;
		setFormsLoading(true);
		void Promise.all(
			targets.map(async (node) => {
				try {
					const forms = await client.listNodeForms(node.id);
					return forms.map((form) => ({ nodeID: node.id, nodeName: node.name, form }));
				} catch {
					// 旧节点没有表单接口，单个节点失败只跳过。
					return [];
				}
			}),
		)
			.then((groups) => {
				if (!cancelled) setGlobalItems(groups.flat());
			})
			.finally(() => {
				if (!cancelled) setFormsLoading(false);
			});
		return () => {
			cancelled = true;
		};
	}, [settings, onlineKey, formsRefresh]);

	const items = useMemo<Item[]>(() => {
		const sessionItems: Item[] = rows.map((row) => ({ kind: "session", row }));
		if (globalItems.length === 0) return sessionItems;
		return [
			...sessionItems,
			{ kind: "global-header" },
			...globalItems.map((item): Item => ({ kind: "global", item })),
		];
	}, [rows, globalItems]);

	if (settingsLoading) {
		return <LoadingScreen text="正在加载待处理…" />;
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

	return (
		<FlatList
			contentContainerStyle={styles.list}
			data={items}
			keyExtractor={(item) => {
				if (item.kind === "session") return `s:${item.row.nodeID}:${item.row.sessionID}`;
				if (item.kind === "global-header") return "global-header";
				return `g:${item.item.nodeID}:${item.item.form.id}`;
			}}
			refreshControl={
				<RefreshControl
					refreshing={loading || formsLoading}
					onRefresh={() => {
						refresh();
						setFormsRefresh((value) => value + 1);
					}}
				/>
			}
			ListHeaderComponent={error ? <Text style={styles.error}>{error}</Text> : undefined}
			renderItem={({ item }) => {
				if (item.kind === "global-header") {
					return <Text style={styles.sectionTitle}>全局表单</Text>;
				}
				if (item.kind === "global") {
					const { nodeID, nodeName, form } = item.item;
					return (
						<Pressable
							style={styles.row}
							onPress={() =>
								router.push(
									`/forms/${encodeURIComponent(nodeID)}/${encodeURIComponent(form.id)}` +
										`?directory=${encodeURIComponent(form.directory ?? "")}`,
								)
							}
						>
							<Text style={styles.title} numberOfLines={1}>
								{form.title || form.id}
							</Text>
							<Text style={styles.meta} numberOfLines={1}>
								{nodeName} · {form.directory || "全局表单"}
							</Text>
						</Pressable>
					);
				}

				const node = nodeByID[item.row.nodeID];
				const swatch = nodeColorSwatch(node?.color) ?? "transparent";
				return (
					<Pressable
						style={[styles.row, { borderLeftColor: swatch }]}
						onPress={() => router.push(sessionRoute(item.row.nodeID, item.row.sessionID))}
					>
						<Text style={styles.title} numberOfLines={1}>
							{item.row.title || item.row.sessionID}
						</Text>
						<Text style={styles.meta} numberOfLines={1}>
							{node?.name ?? item.row.nodeID} · {item.row.count} 条待处理
						</Text>
					</Pressable>
				);
			}}
			ListEmptyComponent={
				<View style={styles.center}>
					<Text style={styles.title}>没有待处理</Text>
					<Text style={styles.hint}>需要你确认或回答的内容会出现在这里。</Text>
				</View>
			}
		/>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		list: { padding: 16, gap: 8 },
		center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
		title: { fontSize: 16, fontWeight: "600", color: colors.text },
		meta: { fontSize: 12, color: colors.textSecondary },
		hint: { color: colors.textSecondary, textAlign: "center" },
		error: { color: colors.danger, marginBottom: 8 },
		sectionTitle: { fontSize: 13, fontWeight: "700", color: colors.textSecondary, marginTop: 8 },
		row: {
			borderWidth: 1,
			borderLeftWidth: 4,
			borderColor: colors.border,
			backgroundColor: colors.surface,
			borderRadius: 10,
			padding: 12,
			gap: 2,
		},
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
