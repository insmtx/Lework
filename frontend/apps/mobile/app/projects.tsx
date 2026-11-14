import type { ProjectSummary } from "@lework/api";
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

import { projectRoute } from "../src/config";
import { orderProjects, projectDisplayName } from "../src/projects/model";
import { useProjects } from "../src/projects/use-projects";
import { useSettings } from "../src/settings/use-settings";

function ProjectCard({ project }: { project: ProjectSummary }) {
	const online = project.nodes.filter((node) => node.online).length;
	const status =
		online > 0 ? `${online}/${project.nodes.length} 个节点在线` : "节点离线";

	return (
		<Pressable
			style={[styles.card, online === 0 && styles.cardOffline]}
			onPress={() => router.push(projectRoute(project.key))}
		>
			<Text style={styles.name} numberOfLines={1}>
				{projectDisplayName(project)}
			</Text>
			<Text style={styles.meta} numberOfLines={1}>
				{project.canonical}
			</Text>
			<Text style={[styles.status, online === 0 && styles.statusOffline]}>{status}</Text>
		</Pressable>
	);
}

/** 项目列表：只展示「我的」之外的项目，「我的」固定在首页。 */
export default function ProjectsScreen() {
	const { settings, loading: settingsLoading } = useSettings();
	const { projects, inbox, loading, error, refresh } = useProjects(settings);

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
				<Text style={styles.title}>未连接</Text>
				<Text style={styles.hint}>先在设置里填写控制面地址与令牌。</Text>
				<Pressable style={styles.button} onPress={() => router.push("/settings")}>
					<Text style={styles.buttonText}>打开设置</Text>
				</Pressable>
			</View>
		);
	}

	const others = orderProjects(
		projects.filter((project) => project.key !== inbox.project?.key),
	);

	return (
		<FlatList
			contentContainerStyle={styles.list}
			data={others}
			keyExtractor={(project) => project.key}
			refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
			ListHeaderComponent={error ? <Text style={styles.error}>{error}</Text> : undefined}
			renderItem={({ item }) => <ProjectCard project={item} />}
			ListEmptyComponent={
				<View style={styles.center}>
					<Text style={styles.title}>还没有其它项目</Text>
					<Text style={styles.hint}>在「我的」里输入的第一条消息会创建项目。</Text>
				</View>
			}
		/>
	);
}

const styles = StyleSheet.create({
	list: { padding: 16, gap: 10 },
	center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
	title: { fontSize: 18, fontWeight: "600" },
	hint: { color: "#666", textAlign: "center" },
	error: { color: "#b91c1c", marginBottom: 8 },
	card: { borderWidth: 1, borderColor: "#e2e8f0", borderRadius: 12, padding: 12, gap: 4 },
	cardOffline: { opacity: 0.6 },
	name: { fontSize: 16, fontWeight: "600" },
	meta: { fontSize: 12, color: "#64748b" },
	status: { fontSize: 12, color: "#166534" },
	statusOffline: { color: "#6b7280" },
	button: {
		marginTop: 8,
		backgroundColor: "#0f172a",
		borderRadius: 8,
		paddingHorizontal: 16,
		paddingVertical: 10,
	},
	buttonText: { color: "#fff", fontWeight: "600" },
});
