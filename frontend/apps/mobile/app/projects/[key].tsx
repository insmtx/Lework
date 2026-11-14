import { router, Stack, useLocalSearchParams } from "expo-router";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
	ActivityIndicator,
	Alert,
	FlatList,
	Modal,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	View,
} from "react-native";

import { createClient } from "../../src/api/client";
import { createSecureDraftStore } from "../../src/composer/secure-draft";
import { newIdempotencyKey, sendToProject } from "../../src/composer/send";
import { projectDisplayName, resolveTarget } from "../../src/projects/model";
import { useProjectSessions } from "../../src/sessions/use-project-sessions";
import { useAppShell, useScreenTitle } from "../../src/ui/app-shell-context";
import { GlobalComposer } from "../../src/ui/global-composer";
import { SessionRow } from "../../src/ui/session-row";

/** 项目页：一个项目跨节点的会话合并展示，主节点可改。 */
export default function ProjectScreen() {
	const params = useLocalSearchParams<{ key?: string | string[] }>();
	const projectKey = Array.isArray(params.key) ? (params.key[0] ?? "") : (params.key ?? "");

	const { settings, settingsLoading, projects: projectsState } = useAppShell();
	const { projects, nodes, loading, error, refresh, setPrimaryNode } = projectsState;
	const store = useMemo(() => createSecureDraftStore(), []);

	const project = useMemo(
		() => projects.find((item) => item.key === projectKey) ?? null,
		[projects, projectKey],
	);
	const projectName = project ? projectDisplayName(project) : "项目";
	useScreenTitle(projectName);
	const nodeNameByID = useMemo(
		() => Object.fromEntries(nodes.map((node) => [node.id, node.name])),
		[nodes],
	);
	const onlineNodeIDs = useMemo(
		() => nodes.filter((node) => node.connected).map((node) => node.id),
		[nodes],
	);

	const onlineProjectNodes = useMemo(
		() => (project?.nodes ?? []).filter((node) => node.online).map((node) => node.node_id),
		[project],
	);
	const sessions = useProjectSessions(settings, project, onlineProjectNodes);

	const target = project ? resolveTarget(project, onlineNodeIDs) : null;
	const nodeID = target?.nodeID ?? null;

	const scopeProjectKey = project?.key ?? "";
	const scopeNodeID = nodeID ?? "";
	const hasScope = scopeProjectKey !== "" && scopeNodeID !== "";

	const [draft, setDraft] = useState("");
	const [sending, setSending] = useState(false);
	const [pickerOpen, setPickerOpen] = useState(false);

	useEffect(() => {
		if (!hasScope) {
			setDraft("");
			return;
		}
		let cancelled = false;
		void store
			.load({ projectKey: scopeProjectKey, nodeID: scopeNodeID })
			.then((value) => {
				if (!cancelled) setDraft(value);
			})
			.catch(() => {
				// 读不到草稿不是致命错误：输入框留空即可。
			});
		return () => {
			cancelled = true;
		};
	}, [store, hasScope, scopeProjectKey, scopeNodeID]);

	const changeText = useCallback(
		(text: string) => {
			setDraft(text);
			if (hasScope) {
				void store.save({ projectKey: scopeProjectKey, nodeID: scopeNodeID }, text).catch(() => {
					// 草稿落盘失败只影响下次恢复，不能阻塞输入。
				});
			}
		},
		[store, hasScope, scopeProjectKey, scopeNodeID],
	);

	const send = useCallback(async () => {
		const text = draft.trim();
		if (!settings || !project || !nodeID || !hasScope || text === "") return;

		setSending(true);
		try {
			const client = createClient(settings);
			const session = await sendToProject(
				client,
				nodeID,
				project.canonical,
				text,
				newIdempotencyKey(nodeID, "new"),
			);
			await store.save({ projectKey: scopeProjectKey, nodeID: scopeNodeID }, "");
			setDraft("");
			sessions.refresh();
			router.push(`/session/${encodeURIComponent(nodeID)}/${encodeURIComponent(session.id)}`);
		} catch (caught) {
			Alert.alert("发送失败", caught instanceof Error ? caught.message : "请稍后重试");
		} finally {
			setSending(false);
		}
	}, [settings, project, nodeID, hasScope, scopeProjectKey, scopeNodeID, draft, store, sessions]);

	const choosePrimary = useCallback(
		(node: string) => {
			setPickerOpen(false);
			void setPrimaryNode(projectKey, node).catch((caught: unknown) => {
				Alert.alert("设置失败", caught instanceof Error ? caught.message : "请稍后重试");
			});
		},
		[projectKey, setPrimaryNode],
	);

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

	if (!project) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>{loading ? "正在加载…" : "项目不存在"}</Text>
				<Text style={styles.hint}>它可能已从所有节点上消失。</Text>
				<Pressable style={styles.button} onPress={() => router.back()}>
					<Text style={styles.buttonText}>返回</Text>
				</Pressable>
			</View>
		);
	}

	const disabledReason =
		nodeID === null
			? onlineProjectNodes.length === 0
				? "节点离线，无法发送"
				: "没有可用节点"
			: undefined;
	const primaryLabel = project.primary_node_id
		? (nodeNameByID[project.primary_node_id] ?? project.primary_node_id)
		: "自动（最近在线）";

	return (
		<View style={styles.screen}>
			<Stack.Screen options={{ title: projectName }} />
			<FlatList
				style={styles.list}
				contentContainerStyle={styles.listContent}
				data={sessions.rows}
				keyExtractor={(row) => `${row.nodeID}:${row.session.id}`}
				refreshControl={
					<RefreshControl
						refreshing={loading || sessions.loading}
						onRefresh={() => {
							refresh();
							sessions.refresh();
						}}
					/>
				}
				ListHeaderComponent={
					<View style={styles.header}>
						<Text style={styles.meta}>{project.canonical}</Text>
						<Pressable style={styles.primary} onPress={() => setPickerOpen(true)}>
							<Text style={styles.primaryText}>主节点：{primaryLabel} ▾</Text>
						</Pressable>
						{onlineProjectNodes.length === 0 ? (
							<Text style={styles.warn}>节点离线，会话列表不可读</Text>
						) : null}
						{error ? <Text style={styles.error}>{error}</Text> : null}
						{sessions.error ? <Text style={styles.error}>{sessions.error}</Text> : null}
					</View>
				}
				renderItem={({ item }) => (
					<SessionRow
						nodeID={item.nodeID}
						session={item.session}
						nodeName={nodeNameByID[item.nodeID]}
					/>
				)}
				ListEmptyComponent={
					<View style={styles.empty}>
						<Text style={styles.hint}>
							{sessions.loading
								? "正在加载会话…"
								: onlineProjectNodes.length === 0
									? "节点离线，暂时读不到会话。"
									: "还没有会话，在下方输入即可开始。"}
						</Text>
					</View>
				}
			/>

			<GlobalComposer
				target={
					nodeID
						? {
								projectKey: project.key,
								projectName: projectDisplayName(project),
								nodeID,
								showNode: false,
							}
						: null
				}
				disabledReason={disabledReason}
				value={draft}
				onChangeText={changeText}
				onSend={send}
				sending={sending}
			/>

			<Modal
				transparent
				visible={pickerOpen}
				animationType="fade"
				onRequestClose={() => setPickerOpen(false)}
			>
				<Pressable style={styles.backdrop} onPress={() => setPickerOpen(false)}>
					<View style={styles.sheet}>
						<Text style={styles.sheetTitle}>选择主节点</Text>
						{project.nodes.map((node) => (
							<Pressable
								key={node.node_id}
								style={styles.sheetItem}
								onPress={() => choosePrimary(node.node_id)}
							>
								<Text style={styles.sheetLabel}>{nodeNameByID[node.node_id] ?? node.node_id}</Text>
								<Text style={node.online ? styles.online : styles.offline}>
									{node.online ? "在线" : "离线"}
								</Text>
							</Pressable>
						))}
					</View>
				</Pressable>
			</Modal>
		</View>
	);
}

const styles = StyleSheet.create({
	screen: { flex: 1, backgroundColor: "#fff" },
	list: { flex: 1 },
	listContent: { padding: 16, gap: 8, flexGrow: 1 },
	header: { gap: 4, marginBottom: 8 },
	center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
	empty: { paddingVertical: 32, alignItems: "center" },
	title: { fontSize: 20, fontWeight: "700" },
	meta: { fontSize: 12, color: "#64748b" },
	hint: { color: "#666", textAlign: "center" },
	warn: { fontSize: 12, color: "#b45309" },
	error: { fontSize: 12, color: "#b91c1c" },
	primary: { alignSelf: "flex-start", paddingVertical: 2 },
	primaryText: { fontSize: 13, color: "#334155" },
	button: {
		marginTop: 8,
		backgroundColor: "#0f172a",
		borderRadius: 8,
		paddingHorizontal: 16,
		paddingVertical: 10,
	},
	buttonText: { color: "#fff", fontWeight: "600" },
	backdrop: { flex: 1, backgroundColor: "rgba(15, 23, 42, 0.25)", justifyContent: "flex-end" },
	sheet: {
		backgroundColor: "#fff",
		borderTopLeftRadius: 16,
		borderTopRightRadius: 16,
		padding: 16,
		gap: 8,
	},
	sheetTitle: { fontSize: 16, fontWeight: "600", marginBottom: 4 },
	sheetItem: {
		flexDirection: "row",
		justifyContent: "space-between",
		paddingVertical: 12,
		borderBottomWidth: 1,
		borderBottomColor: "#f1f5f9",
	},
	sheetLabel: { fontSize: 15 },
	online: { color: "#166534", fontSize: 12 },
	offline: { color: "#6b7280", fontSize: 12 },
});
