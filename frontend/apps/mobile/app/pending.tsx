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

import { sessionRoute } from "../src/config";
import { pendingRows } from "../src/pending/derive";
import type { Theme } from "../src/theme/theme";
import { useTheme, useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";

/** 待审批：从总览快照汇总所有 pending 会话，点进去逐个处理。 */
export default function PendingScreen() {
	useScreenTitle("待审批");
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const {
		settings,
		settingsLoading,
		overviewNodes: nodes,
		overviewLoading: loading,
		overviewError: error,
		refreshOverview: refresh,
	} = useAppShell();

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

	return (
		<FlatList
			contentContainerStyle={styles.list}
			data={rows}
			keyExtractor={(row) => `${row.nodeID}:${row.sessionID}`}
			refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
			ListHeaderComponent={error ? <Text style={styles.error}>{error}</Text> : undefined}
			renderItem={({ item }) => (
				<Pressable
					style={styles.row}
					onPress={() => router.push(sessionRoute(item.nodeID, item.sessionID))}
				>
					<Text style={styles.title} numberOfLines={1}>
						{item.title || item.sessionID}
					</Text>
					<Text style={styles.meta} numberOfLines={1}>
						{item.nodeID} · {item.count} 条待审批
					</Text>
				</Pressable>
			)}
			ListEmptyComponent={
				<View style={styles.center}>
					<Text style={styles.title}>没有待审批</Text>
					<Text style={styles.hint}>需要你确认的操作会出现在这里。</Text>
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
		row: {
			borderWidth: 1,
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
