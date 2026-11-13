import { router } from "expo-router";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
	ActivityIndicator,
	Alert,
	FlatList,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	View,
} from "react-native";

import { createClient } from "../src/api/client";
import { createSecureDraftStore } from "../src/composer/secure-draft";
import { newIdempotencyKey, sendToProject } from "../src/composer/send";
import { INBOX_ALIAS, projectDisplayName } from "../src/projects/model";
import { useProjects } from "../src/projects/use-projects";
import { useProjectSessions } from "../src/sessions/use-project-sessions";
import { useSettings } from "../src/settings/use-settings";
import { GlobalComposer } from "../src/ui/global-composer";
import { SessionRow } from "../src/ui/session-row";

/**
 * 首页就是「我的」：打开 App 直接看到这个项目最近的会话，输入框常驻底部。
 *
 * 「我的」是一条偏好（某节点上的目录），不是 OpenCode 的特殊项目；目录
 * 尚未对应任何项目时也允许发送——第一条消息会把项目创建出来。
 */
export default function InboxScreen() {
	const { settings, loading: settingsLoading } = useSettings();
	const { projects, nodes, dirByNode, inbox, loading, error, refresh, connected } =
		useProjects(settings);
	const store = useMemo(() => createSecureDraftStore(), []);

	const onlineNodeIDs = useMemo(
		() => nodes.filter((node) => node.connected).map((node) => node.id),
		[nodes],
	);
	const nodeNameByID = useMemo(
		() => Object.fromEntries(nodes.map((node) => [node.id, node.name])),
		[nodes],
	);

	const project = inbox.project;
	const inboxDir = inbox.nodeID ? (dirByNode[inbox.nodeID] ?? "") : "";
	const inboxOnline = inbox.nodeID !== null && onlineNodeIDs.includes(inbox.nodeID);

	const projectOnlineNodes = useMemo(
		() =>
			(project?.nodes ?? [])
				.filter((node) => node.online)
				.map((node) => node.node_id),
		[project],
	);
	const sessions = useProjectSessions(
		settings,
		inboxOnline ? project : null,
		projectOnlineNodes,
	);

	const target =
		inbox.nodeID && inboxDir
			? {
					projectKey:
						project?.key ?? `inbox:${inbox.nodeID}:${inboxDir}`,
					projectName: INBOX_ALIAS,
					nodeID: inboxOnline ? inbox.nodeID : null,
					nodeName: inboxOnline ? nodeNameByID[inbox.nodeID] : undefined,
				}
			: null;

	const scope =
		inbox.nodeID && inboxDir
			? { projectKey: target?.projectKey ?? "", nodeID: inbox.nodeID }
			: null;

	const [draft, setDraft] = useState("");
	const [sending, setSending] = useState(false);

	useEffect(() => {
		if (!scope) {
			setDraft("");
			return;
		}
		let cancelled = false;
		void store
			.load(scope)
			.then((value) => {
				if (!cancelled) setDraft(value);
			})
			.catch(() => {
				// 读不到草稿不是致命错误：输入框留空即可。
			});
		return () => {
			cancelled = true;
		};
	}, [store, scope?.projectKey, scope?.nodeID]);

	const changeText = useCallback(
		(text: string) => {
			setDraft(text);
			if (scope) void store.save(scope, text).catch(() => {});
		},
		[scope, store],
	);

	const send = useCallback(async () => {
		const nodeID = target?.nodeID;
		const text = draft.trim();
		if (!settings || !target || !nodeID || !scope || text === "") return;

		setSending(true);
		try {
			const client = createClient(settings);
			const session = await sendToProject(
				client,
				nodeID,
				inboxDir,
				text,
				newIdempotencyKey(nodeID, "new"),
			);
			await store.save(scope, "");
			setDraft("");
			refresh();
			sessions.refresh();
			router.push(`/session/${encodeURIComponent(nodeID)}/${encodeURIComponent(session.id)}`);
		} catch (caught) {
			// 发送失败保留草稿：用户重试时不该重新打一遍。
			Alert.alert("发送失败", caught instanceof Error ? caught.message : "请稍后重试");
		} finally {
			setSending(false);
		}
	}, [settings, target, scope, draft, inboxDir, store, refresh, sessions]);

	const disabledReason = !settings
		? "未连接控制面"
		: inbox.nodeID === null
			? "还没有可用的「我的」目录"
			: !inboxDir
				? "「我的」目录不可用"
				: !inboxOnline
					? "我的节点离线"
					: undefined;

	const onRefresh = useCallback(() => {
		refresh();
		sessions.refresh();
	}, [refresh, sessions]);

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

	return (
		<View style={styles.screen}>
			<FlatList
				style={styles.list}
				contentContainerStyle={styles.listContent}
				data={sessions.rows}
				keyExtractor={(row) => `${row.nodeID}:${row.session.id}`}
				refreshControl={
					<RefreshControl
						refreshing={loading || sessions.loading}
						onRefresh={onRefresh}
					/>
				}
				ListHeaderComponent={
					<View style={styles.header}>
						<Text style={styles.title}>{INBOX_ALIAS}</Text>
						<Text style={styles.meta} numberOfLines={1}>
							{project
								? `${projectDisplayName(project)} · ${project.canonical}`
								: inboxDir
									? `${inboxDir}（发送后自动创建项目）`
									: "尚未在此节点初始化"}
						</Text>
						{!connected ? <Text style={styles.warn}>事件流未连接，正在重试</Text> : null}
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
							{sessions.loading ? "正在加载会话…" : "还没有会话，在下方输入即可开始。"}
						</Text>
					</View>
				}
			/>
			<GlobalComposer
				target={target}
				disabledReason={disabledReason}
				value={draft}
				onChangeText={changeText}
				onSend={send}
				sending={sending}
			/>
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
	hint: { color: "#666", textAlign: "center" },
	meta: { fontSize: 12, color: "#64748b" },
	warn: { fontSize: 12, color: "#b45309" },
	error: { fontSize: 12, color: "#b91c1c" },
	button: {
		marginTop: 8,
		backgroundColor: "#0f172a",
		borderRadius: 8,
		paddingHorizontal: 16,
		paddingVertical: 10,
	},
	buttonText: { color: "#fff", fontWeight: "600" },
});
