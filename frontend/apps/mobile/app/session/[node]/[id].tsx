import type { Message, PendingPermission, Session } from "@lework/api";
import { router, useLocalSearchParams } from "expo-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	FlatList,
	Modal,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	TextInput,
	View,
} from "react-native";

import { sessionRoute } from "../../../src/config";
import { nodeColorSwatch } from "../../../src/nodes/colors";
import { findProjectForSession, projectDirectoryOnNode } from "../../../src/projects/model";
import { FormCard } from "../../../src/session/form-card";
import { MessageRow } from "../../../src/session/message-row";
import { filterVisibleMessages } from "../../../src/session/messages";
import { runSend } from "../../../src/session/send-flow";
import { statusColor, statusTone } from "../../../src/session/status";
import { SessionStatusIcon } from "../../../src/session/status-icon";
import { useAutoFollow } from "../../../src/session/use-auto-follow";
import { useSession } from "../../../src/session/use-session";
import { selectSiblingSessions } from "../../../src/sessions/select";
import { useNodeSessions } from "../../../src/sessions/use-node-sessions";
import { formatRelativeTime } from "../../../src/sidebar/model";
import type { Theme } from "../../../src/theme/theme";
import { useThemePreference } from "../../../src/theme/theme-context";
import { useTheme, useThemedStyles } from "../../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../../src/ui/app-shell-context";
import { GlobalComposer } from "../../../src/ui/global-composer";
import { BrandSpinner, LoadingScreen } from "../../../src/ui/loading";
import { RowSweep } from "../../../src/ui/row-sweep";
import { ScreenTopBar } from "../../../src/ui/top-bar";

function param(value: string | string[] | undefined): string {
	return Array.isArray(value) ? (value[0] ?? "") : (value ?? "");
}

// 用分隔组件而不是 contentContainerStyle.gap：虚拟列表的 spacer 也是直接子节点，
// gap 会随渲染窗口变化凭空改变内容高度，贴底滚动就会被带着抖。
function MessageSeparator() {
	return <View style={{ height: 12 }} />;
}

function ApprovalCard({
	permission,
	onReply,
}: {
	permission: PendingPermission;
	onReply: (decision: "once" | "always" | "reject", message?: string) => Promise<void>;
}) {
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const [rejecting, setRejecting] = useState(false);
	const [reason, setReason] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const respond = async (decision: "once" | "always" | "reject") => {
		setBusy(true);
		setError(null);
		try {
			await onReply(decision, decision === "reject" ? reason.trim() || undefined : undefined);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "failed to reply");
		} finally {
			setBusy(false);
		}
	};

	return (
		<View style={styles.approval}>
			<Text style={styles.approvalTitle}>Approval required</Text>
			<Text style={styles.approvalAction}>{permission.action || permission.id}</Text>
			{permission.message ? <Text style={styles.approvalMessage}>{permission.message}</Text> : null}
			{permission.resources.length > 0 ? (
				<View style={styles.resources}>
					{permission.resources.slice(0, 6).map((resource) => (
						<Text key={resource} style={styles.resource} numberOfLines={2}>
							{resource}
						</Text>
					))}
				</View>
			) : null}

			{rejecting ? (
				<TextInput
					style={styles.input}
					value={reason}
					onChangeText={setReason}
					placeholder="Reason (optional)"
					placeholderTextColor={theme.colors.placeholder}
					selectionColor={theme.colors.accent}
					editable={!busy}
				/>
			) : null}

			{error ? <Text style={styles.error}>{error}</Text> : null}

			<View style={styles.approvalButtons}>
				<Pressable
					style={[styles.primaryButton, busy && styles.disabled]}
					disabled={busy}
					onPress={() => void respond("once")}
				>
					<Text style={styles.primaryButtonText}>Allow once</Text>
				</Pressable>
				<Pressable
					style={[styles.secondaryButton, busy && styles.disabled]}
					disabled={busy}
					onPress={() => void respond("always")}
				>
					<Text style={styles.secondaryButtonText}>Always</Text>
				</Pressable>
				<Pressable
					style={[styles.rejectButton, busy && styles.disabled]}
					disabled={busy}
					onPress={() => {
						if (rejecting) {
							void respond("reject");
						} else {
							setRejecting(true);
						}
					}}
				>
					<Text style={styles.rejectButtonText}>{rejecting ? "Reject" : "Reject…"}</Text>
				</Pressable>
			</View>
		</View>
	);
}

function SessionSwitcher({
	sessions,
	currentID,
	loading,
	onPick,
	onClose,
}: {
	sessions: Session[];
	currentID: string;
	loading: boolean;
	onPick: (id: string) => void;
	onClose: () => void;
}) {
	const styles = useThemedStyles(createStyles);

	return (
		<Pressable style={styles.backdrop} onPress={onClose}>
			<View style={styles.sheet}>
				<Text style={styles.sheetTitle}>切换会话</Text>
				<FlatList
					data={sessions}
					keyExtractor={(item) => item.id}
					renderItem={({ item }) => (
						<Pressable style={styles.sheetItem} onPress={() => onPick(item.id)}>
							<Text
								style={[styles.sheetLabel, item.id === currentID && styles.sheetLabelActive]}
								numberOfLines={1}
							>
								{item.title || item.id}
							</Text>
							<Text style={styles.sheetTime}>{formatRelativeTime(item.updatedAt)}</Text>
						</Pressable>
					)}
					ListEmptyComponent={
						loading ? (
							<BrandSpinner size={20} style={styles.sheetLoading} />
						) : (
							<Text style={styles.hint}>这个项目下还没有会话</Text>
						)
					}
				/>
			</View>
		</Pressable>
	);
}

export default function SessionScreen() {
	const params = useLocalSearchParams<{ node: string; id: string; form?: string }>();
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const { preferences } = useThemePreference();
	const nodeID = param(params.node);
	const sessionID = param(params.id);
	const deepForm = param(params.form);

	const { settings, settingsLoading, projects, overviewNodes, sessionState, markRead } =
		useAppShell();
	const {
		messages,
		permissions,
		forms,
		loading,
		error,
		refresh,
		reply,
		replyForm,
		cancelForm,
		send,
		interrupt,
		sending,
		notFound,
		state: localState,
		hasNewBelow,
		following,
		setFollowing,
		jumpToLatest,
	} = useSession(settings, nodeID, sessionID);

	const nodeSessions = useNodeSessions(settings, nodeID);
	const current = useMemo(
		() => nodeSessions.sessions.find((session) => session.id === sessionID) ?? null,
		[nodeSessions.sessions, sessionID],
	);
	const project = useMemo(
		() => (current ? findProjectForSession(current, projects.projects) : null),
		[current, projects.projects],
	);
	const title = current?.title || "会话";
	useScreenTitle(title);

	// 顶栏圆点标识当前会话属于哪台电脑；颜色只做机器身份，不替代状态。
	const nodeColor = useMemo(
		() => nodeColorSwatch(projects.nodes.find((node) => node.id === nodeID)?.color),
		[projects.nodes, nodeID],
	);

	// overview 命中时以它为准：它是服务端事件投影，本地事件断档后仍能自愈。
	const status = useMemo(() => {
		const fromOverview = sessionState(nodeID, sessionID);
		if (fromOverview) return fromOverview;
		const node = overviewNodes.find((candidate) => candidate.node_id === nodeID);
		if (node && !node.connected) return "offline";
		return localState;
	}, [sessionState, overviewNodes, nodeID, sessionID, localState]);

	// 「执行完成」只短暂显示：常驻会把已完成误读成仍在跑。
	const [idleHidden, setIdleHidden] = useState(false);
	useEffect(() => {
		setIdleHidden(false);
		if (status !== "idle") return;
		const timer = setTimeout(() => setIdleHidden(true), 5_000);
		return () => clearTimeout(timer);
	}, [status]);
	const shownStatus = idleHidden ? null : status;
	const tone = statusTone(shownStatus);

	const siblings = useMemo(() => {
		if (!current) return [];
		// 项目还没聚合出来时退回会话自身的 projectID 与目录，至少不跨项目。
		const key =
			project?.key ??
			(current.projectID === "global"
				? `global:${current.directory ?? ""}`
				: (current.projectID ?? ""));
		// 会话目录就在这台节点上，前缀匹配必须用该节点自己的检出路径。
		const canonical = project ? projectDirectoryOnNode(project, nodeID) : (current.directory ?? "");
		if (!key && !canonical) return [current];
		return selectSiblingSessions(nodeSessions.sessions, key, canonical);
	}, [current, project, nodeID, nodeSessions.sessions]);

	const [draft, setDraft] = useState("");
	const [actionError, setActionError] = useState<string | null>(null);
	const [switcherOpen, setSwitcherOpen] = useState(false);

	// idle 结束标记与只有被隐藏的过程内容的消息不占位，列表里只剩真正可读的内容。
	const visibleMessages = useMemo(
		() => filterVisibleMessages(messages, preferences),
		[messages, preferences],
	);

	// 已读水位只在「贴底且内容有更新」时推进，并且节流 5s：
	// 流式文本每几毫秒就变一次，每次都写 keystore 既费电又没必要。
	const lastMarkedAt = useRef(0);
	const markReadNow = useCallback(() => {
		const now = Date.now();
		if (now - lastMarkedAt.current < 5_000) return;
		lastMarkedAt.current = now;
		markRead(nodeID, sessionID, now);
	}, [markRead, nodeID, sessionID]);

	// 贴底跟随统一交给 useAutoFollow：用真实内容高度定位，只认手指滚动，
	// 避免 FlatList 的估算末尾在流式期间把消息流来回拽动。
	const {
		listRef,
		handlers: listHandlers,
		follow: followLatest,
		reset: resetFollow,
	} = useAutoFollow<Message>({
		following,
		setFollowing,
		// 用户自己滚回底部也算看完当前内容，水位继续前进。
		onAtBottom: markReadNow,
	});

	// 从通知带 ?form= 进来时停在底部：待处理卡片就在输入框上方，不再把会话拽回顶部。
	// 用 ref 去重，避免 resync/刷新把已经往上翻历史的人再次拽回底部。
	const deepFormHandled = useRef<string | null>(null);
	useEffect(() => {
		if (!deepForm || loading || deepFormHandled.current === deepForm) return;
		deepFormHandled.current = deepForm;
		followLatest();
	}, [deepForm, loading, followLatest]);

	// messages 的变化也包括「列表长度不变但文本被增量替换」，只看长度会漏掉
	// 流式推进；这里的依赖是故意多出来的。
	// biome-ignore lint/correctness/useExhaustiveDependencies: 需要响应文本内容变化
	useEffect(() => {
		if (loading || visibleMessages.length === 0 || !following) return;
		markReadNow();
	}, [loading, visibleMessages.length, messages, following, markReadNow]);

	// biome-ignore lint/correctness/useExhaustiveDependencies: 切换会话时要重置跟随，而不是等 ref 变化
	useEffect(() => {
		resetFollow();
	}, [nodeID, sessionID, resetFollow]);

	useEffect(() => {
		if (switcherOpen) nodeSessions.refresh();
	}, [switcherOpen, nodeSessions.refresh]);

	const submit = async () => {
		const text = draft.trim();
		if (!text) return;
		setActionError(null);
		// 主动发言后回到最新一条，即使刚才正在往上翻历史。
		followLatest();
		try {
			await runSend(
				() => send(text),
				() => setDraft(""),
			);
		} catch (caught) {
			setActionError(caught instanceof Error ? caught.message : "failed to send");
		}
	};

	const stop = async () => {
		setActionError(null);
		try {
			await interrupt();
		} catch (caught) {
			setActionError(caught instanceof Error ? caught.message : "failed to interrupt");
		}
	};

	if (settingsLoading) {
		return <LoadingScreen text="正在打开会话…" />;
	}

	if (!settings) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>Not connected</Text>
				<Text style={styles.hint}>Configure the control plane in Settings first.</Text>
				<Pressable style={styles.primaryButton} onPress={() => router.push("/settings")}>
					<Text style={styles.primaryButtonText}>Open settings</Text>
				</Pressable>
			</View>
		);
	}

	if (notFound) {
		return (
			<View style={styles.center}>
				<Text style={styles.title}>会话不存在</Text>
				<Text style={styles.hint}>它可能已被删除，或这条深链已经过期。</Text>
				<Pressable style={styles.primaryButton} onPress={() => router.replace("/pending")}>
					<Text style={styles.primaryButtonText}>去待处理</Text>
				</Pressable>
				<Pressable style={styles.secondaryButton} onPress={() => router.replace("/")}>
					<Text style={styles.secondaryButtonText}>回首页</Text>
				</Pressable>
			</View>
		);
	}

	return (
		<View style={styles.screen}>
			<ScreenTopBar
				title={title}
				onTitlePress={() => setSwitcherOpen(true)}
				sweep={<RowSweep state={shownStatus} color={nodeColor} />}
				status={
					<View style={styles.topStatus}>
						{nodeColor ? <View style={[styles.nodeDot, { backgroundColor: nodeColor }]} /> : null}
						<SessionStatusIcon state={shownStatus} color={statusColor(theme, tone)} size={15} />
					</View>
				}
				extra={
					<Pressable style={styles.stopButton} onPress={() => void stop()}>
						<Text style={styles.stopText}>停止</Text>
					</Pressable>
				}
			/>

			{error ? <Text style={styles.error}>{error}</Text> : null}
			{actionError ? <Text style={styles.error}>{actionError}</Text> : null}

			<View style={styles.messagesWrap}>
				<FlatList
					ref={listRef}
					style={styles.messages}
					contentContainerStyle={styles.messagesContent}
					data={visibleMessages}
					keyExtractor={(message) => message.id}
					// 字号与展示偏好变化时，行内内容必须一起重排；字号通过主题样式与 context 自动更新。
					extraData={`${preferences.toolDisplay}:${preferences.processDisplay}`}
					renderItem={({ item, index }) => (
						<MessageRow
							message={item}
							isLastMessage={index === visibleMessages.length - 1}
							nodeID={nodeID}
							sessionID={sessionID}
						/>
					)}
					refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
					ItemSeparatorComponent={MessageSeparator}
					{...listHandlers}
					scrollEventThrottle={16}
					ListFooterComponent={
						forms.length + permissions.length > 0 ? (
							<View style={styles.approvals}>
								{forms.map((form) => (
									<FormCard
										key={form.id}
										form={form}
										onSubmit={(answer) => replyForm(form.id, answer)}
										onCancel={() => cancelForm(form.id)}
									/>
								))}
								{permissions.map((permission) => (
									<ApprovalCard
										key={permission.id}
										permission={permission}
										onReply={(decision, message) => reply(permission.id, decision, message)}
									/>
								))}
							</View>
						) : undefined
					}
					ListEmptyComponent={
						loading ? undefined : (
							<View style={styles.center}>
								<Text style={styles.hint}>No messages yet.</Text>
							</View>
						)
					}
				/>

				{hasNewBelow ? (
					<View style={styles.newContentWrap} pointerEvents="box-none">
						<Pressable
							style={styles.newContent}
							onPress={() => {
								jumpToLatest();
								followLatest();
							}}
						>
							<Text style={styles.newContentText}>有新内容 ·</Text>
						</Pressable>
					</View>
				) : null}
			</View>

			{forms.length + permissions.length > 0 ? (
				<View style={styles.pendingWrap}>
					<Pressable
						accessibilityRole="button"
						onPress={() => {
							jumpToLatest();
							followLatest();
						}}
					>
						<Text style={styles.pendingBarText}>
							有待处理 · {forms.length + permissions.length}
						</Text>
					</Pressable>
				</View>
			) : null}

			<GlobalComposer
				target={{
					projectKey: project?.key ?? `${nodeID}:${sessionID}`,
					projectName: title,
					nodeID,
					showNode: false,
				}}
				showChip={false}
				value={draft}
				onChangeText={setDraft}
				onSend={submit}
				sending={sending}
			/>

			<Modal
				transparent
				visible={switcherOpen}
				animationType="fade"
				onRequestClose={() => setSwitcherOpen(false)}
			>
				<SessionSwitcher
					sessions={siblings}
					currentID={sessionID}
					loading={nodeSessions.loading}
					onPick={(id) => {
						setSwitcherOpen(false);
						if (id !== sessionID) router.replace(sessionRoute(nodeID, id));
					}}
					onClose={() => setSwitcherOpen(false)}
				/>
			</Modal>
		</View>
	);
}

function createStyles(theme: Theme) {
	const { colors } = theme;
	return StyleSheet.create({
		screen: { flex: 1, backgroundColor: colors.background },
		topStatus: { flexDirection: "row", alignItems: "center", gap: 5 },
		nodeDot: { width: 8, height: 8, borderRadius: 4 },
		center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
		title: { fontSize: 18, fontWeight: "600", color: colors.text },
		hint: { color: colors.textSecondary, textAlign: "center" },
		error: { color: colors.danger, paddingHorizontal: 16, paddingTop: 8 },
		stopButton: { paddingHorizontal: 6, paddingVertical: 6 },
		stopText: { fontSize: 13, color: colors.textSecondary },
		messagesWrap: { flex: 1 },
		messages: { flex: 1 },
		messagesContent: { flexGrow: 1, justifyContent: "flex-end", padding: 16 },
		newContentWrap: { position: "absolute", left: 0, right: 0, bottom: 10, alignItems: "center" },
		newContent: {
			borderRadius: 14,
			paddingHorizontal: 12,
			paddingVertical: 6,
			backgroundColor: colors.action,
		},
		newContentText: { fontSize: 12, fontWeight: "600", color: colors.actionText },
		approvals: { gap: 10, marginTop: 12, marginBottom: 12 },
		pendingWrap: { alignItems: "center", paddingBottom: 8 },
		pendingBarText: {
			borderRadius: 14,
			paddingHorizontal: 12,
			paddingVertical: 6,
			overflow: "hidden",
			backgroundColor: colors.action,
			color: colors.actionText,
			fontSize: 12,
			fontWeight: "600",
		},
		approval: {
			borderWidth: 1,
			borderColor: colors.warningBorder,
			backgroundColor: colors.warningSurface,
			borderRadius: 12,
			padding: 12,
			gap: 8,
		},
		approvalTitle: { fontSize: 14, fontWeight: "700", color: colors.warning },
		approvalAction: { fontSize: 15, fontWeight: "600", color: colors.text },
		approvalMessage: { fontSize: 13, color: colors.textSecondary },
		resources: { gap: 4 },
		resource: {
			fontFamily: "monospace",
			fontSize: 12,
			color: colors.textStrong,
			backgroundColor: colors.surfaceSubtle,
			borderRadius: 6,
			paddingHorizontal: 8,
			paddingVertical: 6,
		},
		approvalButtons: { flexDirection: "row", flexWrap: "wrap", gap: 8, marginTop: 4 },
		input: {
			borderWidth: 1,
			borderColor: colors.inputBorder,
			borderRadius: 8,
			paddingHorizontal: 10,
			paddingVertical: 8,
			fontSize: 14,
			backgroundColor: colors.inputBackground,
			color: colors.text,
		},
		primaryButton: {
			backgroundColor: colors.action,
			borderRadius: 8,
			paddingHorizontal: 14,
			paddingVertical: 10,
			alignItems: "center",
			justifyContent: "center",
		},
		primaryButtonText: { color: colors.actionText, fontWeight: "600" },
		secondaryButton: {
			borderWidth: 1,
			borderColor: colors.borderStrong,
			borderRadius: 8,
			paddingHorizontal: 12,
			paddingVertical: 8,
			alignItems: "center",
		},
		secondaryButtonText: { color: colors.text, fontWeight: "600" },
		rejectButton: {
			borderWidth: 1,
			borderColor: colors.danger,
			borderRadius: 8,
			paddingHorizontal: 12,
			paddingVertical: 8,
			alignItems: "center",
		},
		rejectButtonText: { color: colors.danger, fontWeight: "600" },
		disabled: { opacity: 0.5 },
		backdrop: { flex: 1, backgroundColor: colors.modalBackdrop, justifyContent: "flex-end" },
		sheet: {
			backgroundColor: colors.surface,
			borderTopLeftRadius: 16,
			borderTopRightRadius: 16,
			padding: 16,
			gap: 8,
			maxHeight: "65%",
		},
		sheetTitle: { fontSize: 16, fontWeight: "600", marginBottom: 4, color: colors.text },
		sheetItem: {
			flexDirection: "row",
			alignItems: "center",
			justifyContent: "space-between",
			gap: 12,
			paddingVertical: 12,
			borderBottomWidth: 1,
			borderBottomColor: colors.borderSubtle,
		},
		sheetLabel: { flex: 1, fontSize: 15, color: colors.text },
		sheetLabelActive: { fontWeight: "700", color: colors.accent },
		sheetTime: { fontSize: 11, color: colors.textMuted },
		sheetLoading: { paddingVertical: 24 },
	});
}
