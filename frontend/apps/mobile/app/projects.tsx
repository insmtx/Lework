import type { ProjectSummary } from "@lework/api";
import { router } from "expo-router";
import {
	FlatList,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	View,
} from "react-native";

import { projectRoute } from "../src/config";
import { projectDisplayName } from "../src/projects/model";
import { formatRelativeTime } from "../src/sidebar/model";
import type { Theme } from "../src/theme/theme";
import { useThemedStyles } from "../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../src/ui/app-shell-context";
import { LoadingScreen } from "../src/ui/loading";

function ProjectCard({
	project,
	lastActivityAt,
}: {
	project: ProjectSummary;
	lastActivityAt: number;
}) {
	const styles = useThemedStyles(createStyles);
	const online = project.nodes.some((node) => node.online);

	return (
		<Pressable
			style={[styles.card, !online && styles.cardOffline]}
			onPress={() => router.push(projectRoute(project.key))}
		>
			<Text style={styles.name} numberOfLines={1}>
				{projectDisplayName(project)}
			</Text>
			<Text style={styles.meta} numberOfLines={1}>
				{project.canonical}
			</Text>
			<Text style={[styles.status, !online && styles.statusOffline]}>
				{!online ? "节点离线" : lastActivityAt ? formatRelativeTime(lastActivityAt) : "暂无会话"}
			</Text>
		</Pressable>
	);
}

/** 项目列表：「我的」已在侧边栏和首页，这里列出其余项目。 */
export default function ProjectsScreen() {
	useScreenTitle("项目");
	const styles = useThemedStyles(createStyles);
	const { settings, settingsLoading, projects, groups } = useAppShell();
	const { projects: allProjects, inbox, loading, error, refresh } = projects;

	if (settingsLoading) {
		return (
			<LoadingScreen text="正在加载项目…" />
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

	// 与抽屉共用同一份排序：groups 已按「组内最新会话时间」排好，直接取
	// 名次即可，不需要在这里重复一遍聚合逻辑。
	const rankOf = new Map(groups.map((group, index) => [group.project.key, index]));
	const activityOf = new Map(groups.map((group) => [group.project.key, group.lastActivityAt]));
	const others = allProjects
		.filter((project) => project.key !== inbox.project?.key)
		.sort(
			(left, right) =>
				(rankOf.get(left.key) ?? Number.MAX_SAFE_INTEGER) -
				(rankOf.get(right.key) ?? Number.MAX_SAFE_INTEGER),
		);

	return (
		<FlatList
			contentContainerStyle={styles.list}
			data={others}
			keyExtractor={(project) => project.key}
			refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
			ListHeaderComponent={error ? <Text style={styles.error}>{error}</Text> : undefined}
			renderItem={({ item }) => (
				<ProjectCard project={item} lastActivityAt={activityOf.get(item.key) ?? 0} />
			)}
			ListEmptyComponent={
				<View style={styles.center}>
					<Text style={styles.title}>还没有其它项目</Text>
					<Text style={styles.hint}>在「我的」里输入的第一条消息会创建项目。</Text>
				</View>
			}
		/>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		list: { padding: 16, gap: 10 },
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
		cardOffline: { opacity: 0.6 },
		name: { fontSize: 16, fontWeight: "600", color: colors.text },
		meta: { fontSize: 12, color: colors.textSecondary },
		status: { fontSize: 12, color: colors.success },
		statusOffline: { color: colors.offline },
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
