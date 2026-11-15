import type { Message, PendingPermission, Session } from "@lework/api";
import { router, useLocalSearchParams } from "expo-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
	ActivityIndicator,
	FlatList,
	Modal,
	type NativeScrollEvent,
	type NativeSyntheticEvent,
	Pressable,
	RefreshControl,
	StyleSheet,
	Text,
	TextInput,
	View,
} from "react-native";

import { sessionRoute } from "../../../src/config";
import { findProjectForSession } from "../../../src/projects/model";
import { MarkdownBody } from "../../../src/session/markdown";
import {
	messageText,
	rendersMarkdown,
	shouldRenderMessage,
	toolParts,
} from "../../../src/session/messages";
import { reasoningText, visibleReasoning } from "../../../src/session/process";
import { isNearBottom } from "../../../src/session/scroll";
import { runSend } from "../../../src/session/send-flow";
import { type StatusTone, statusLabel, statusTone } from "../../../src/session/status";
import { useSession } from "../../../src/session/use-session";
import { selectSiblingSessions } from "../../../src/sessions/select";
import { useNodeSessions } from "../../../src/sessions/use-node-sessions";
import { CHAT_FONT_SCALE } from "../../../src/settings/preferences";
import { formatRelativeTime } from "../../../src/sidebar/model";
import type { Theme } from "../../../src/theme/theme";
import { useThemePreference } from "../../../src/theme/theme-context";
import { useTheme, useThemedStyles } from "../../../src/theme/use-theme";
import { useAppShell, useScreenTitle } from "../../../src/ui/app-shell-context";
import { GlobalComposer } from "../../../src/ui/global-composer";
import { ToolCallCard } from "../../../src/ui/tool-call/tool-call-card";
import { ScreenTopBar } from "../../../src/ui/top-bar";

function param(value: string | string[] | undefined): string {
	return Array.isArray(value) ? (value[0] ?? "") : (value ?? "");
}

/** statusColor 把状态条的语气映射到主题色，与行内状态点保持同一套语义。 */
function statusColor(theme: Theme, tone: StatusTone): string {
	switch (tone) {
		case "info":
			return theme.colors.accent;
		case "warn":
			return theme.colors.warning;
		case "error":
			return theme.colors.danger;
		default:
			return theme.colors.offline;
	}
}

/**
 * ReasoningCard 展示思考过程：brief 先给摘要，点开看完整；full 默认展开。
 *
 * 它和正文分开渲染：思考不是回复内容，塞进气泡会让用户以为模型说过这些话。
 */
function ReasoningCard({ message, mode }: { message: Message; mode: "brief" | "full" }) {
	const styles = useThemedStyles(createStyles);
	const [expanded, setExpanded] = useState(mode === "full");
	const text = reasoningText(message);
	const preview = visibleReasoning(message, "brief");

	if (!text) return null;

	return (
		<View style={styles.reasoningCard}>
			<Pressable
				style={styles.reasoningHeader}
				onPress={() => setExpanded((open) => !open)}
				accessibilityLabel="展开或收起思考过程"
			>
				<Text style={styles.reasoningChevron}>{expanded ? "▾" : "▸"}</Text>
				<Text style={styles.reasoningLabel}>思考过程</Text>
			</Pressable>
			<Text style={styles.reasoningBody} numberOfLines={expanded ? undefined : 2}>
				{expanded ? text : preview}
			</Text>
		</View>
	);
}

function MessageRow({
	message,
	fontScale,
	nodeID,
}: {
	message: Message;
	fontScale: number;
	nodeID: string;
}) {
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const { preferences } = useThemePreference();

	// 失败的 idle 是唯一会进入列表的 idle 消息：只留一行错误，不显示状态词。
	if (message.role === "idle") {
		return (
			<View style={styles.noticeRow}>
				<Text style={styles.noticeError}>{message.error?.message ?? "本轮执行失败"}</Text>
			</View>
		);
	}

	const isUser = message.role === "user";
	const text = messageText(message);
	const tools = preferences.toolDisplay === "hidden" ? [] : toolParts(message);
	// 助手回复可能是 Markdown；用户输入保持纯文本。
	const markdown = rendersMarkdown(message) && text !== "";
	const reasoning =
		!isUser && preferences.processDisplay !== "hidden" && reasoningText(message) !== "";

	return (
		<View style={[styles.turn, isUser ? styles.userTurn : styles.assistantTurn]}>
			{text ? (
				<View style={[styles.bubble, isUser ? styles.userBubble : styles.assistantBubble]}>
					{markdown ? (
						<MarkdownBody content={text} scheme={theme.scheme} scale={fontScale} />
					) : (
						<Text
							style={[
								styles.messageText,
								{
									fontSize: Math.round(15 * fontScale),
									lineHeight: Math.round(21 * fontScale),
								},
								isUser && styles.userMessageText,
							]}
						>
							{text}
						</Text>
					)}
				</View>
			) : null}
			{reasoning ? (
				<ReasoningCard
					message={message}
					mode={preferences.processDisplay === "full" ? "full" : "brief"}
				/>
			) : null}
			{!isUser && tools.length > 0 ? (
				<View style={styles.tools}>
					{tools.map((part, index) => (
						<ToolCallCard
							key={part.callID || `${part.tool || "tool"}-${index}`}
							part={part}
							defaultExpanded={preferences.toolDisplay === "expanded"}
							nodeID={nodeID}
						/>
					))}
				</View>
			) : null}
			{message.error ? <Text style={styles.turnError}>{message.error.message}</Text> : null}
		</View>
	);
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
	const theme = useTheme();
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
							<ActivityIndicator style={styles.sheetLoading} color={theme.colors.accent} />
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
	const params = useLocalSearchParams<{ node: string; id: string }>();
	const theme = useTheme();
	const styles = useThemedStyles(createStyles);
	const { preferences } = useThemePreference();
	const fontScale = CHAT_FONT_SCALE[preferences.chatFontSize];
	const nodeID = param(params.node);
	const sessionID = param(params.id);

	const { settings, settingsLoading, projects, overviewNodes, sessionState, markRead } =
		useAppShell();
	const {
		messages,
		permissions,
		loading,
		error,
		refresh,
		reply,
		send,
		interrupt,
		sending,
		notFound,
		state: localState,
		hasNewBelow,
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
		const canonical = project?.canonical ?? current.directory ?? "";
		if (!key && !canonical) return [current];
		return selectSiblingSessions(nodeSessions.sessions, key, canonical);
	}, [current, project, nodeSessions.sessions]);

	const [draft, setDraft] = useState("");
	const [actionError, setActionError] = useState<string | null>(null);
	const [switcherOpen, setSwitcherOpen] = useState(false);

	// idle 结束标记与只有被隐藏的过程内容的消息不占位，列表里只剩真正可读的内容。
	const visibleMessages = useMemo(
		() =>
			messages.filter((message) => {
				if (message.role === "idle") return shouldRenderMessage(message);
				if (messageText(message) !== "") return true;
				if (message.error) return true;
				if (preferences.toolDisplay !== "hidden" && toolParts(message).length > 0) return true;
				return visibleReasoning(message, preferences.processDisplay) !== null;
			}),
		[messages, preferences.processDisplay, preferences.toolDisplay],
	);

	const listRef = useRef<FlatList<Message>>(null);
	// 用户手动翻过历史后，流式更新不能把它拽回底部。
	const userScrolled = useRef(false);
	const stickToBottom = useRef(true);

	// 已读水位只在「贴底且内容有更新」时推进，并且节流 5s：
	// 流式文本每几毫秒就变一次，每次都写 keystore 既费电又没必要。
	const lastMarkedAt = useRef(0);
	const markReadIfFollowing = useCallback(() => {
		if (!stickToBottom.current) return;
		const now = Date.now();
		if (now - lastMarkedAt.current < 5_000) return;
		lastMarkedAt.current = now;
		markRead(nodeID, sessionID, now);
	}, [markRead, nodeID, sessionID]);

	// messages 的变化也包括「列表长度不变但文本被增量替换」，只看长度会漏掉
	// 流式推进；这里的依赖是故意多出来的。
	// biome-ignore lint/correctness/useExhaustiveDependencies: 需要响应文本内容变化
	useEffect(() => {
		if (loading || visibleMessages.length === 0) return;
		markReadIfFollowing();
	}, [loading, visibleMessages.length, messages, markReadIfFollowing]);

	// biome-ignore lint/correctness/useExhaustiveDependencies: 切换会话时要重置滚动状态，而不是等 ref 变化
	useEffect(() => {
		userScrolled.current = false;
		stickToBottom.current = true;
	}, [nodeID, sessionID]);

	const scrollToLatest = (animated: boolean) => {
		if (visibleMessages.length === 0) return;
		listRef.current?.scrollToEnd({ animated });
	};

	// 内容长高时保持贴底：首次加载直接落在最底部（再往下拉不动），之后只在
	// 用户本来就贴着底部时才跟随，向上翻历史时保持原位。
	const handleContentSizeChange = () => {
		if (visibleMessages.length === 0) return;
		if (userScrolled.current && !stickToBottom.current) return;
		// 首屏虚拟列表会分多次测量，无动画地多贴一次底，避免停在半路。
		scrollToLatest(userScrolled.current);
		if (!userScrolled.current) {
			requestAnimationFrame(() => scrollToLatest(false));
		}
	};

	const handleScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
		const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
		stickToBottom.current = isNearBottom({
			offsetY: contentOffset.y,
			contentHeight: contentSize.height,
			viewportHeight: layoutMeasurement.height,
		});
		// 把跟随状态同步给 useSession：暂停期间的新内容只亮「有新内容」提示。
		setFollowing(stickToBottom.current);
		// 滚回底部即视为看完当前内容，水位继续前进。
		if (stickToBottom.current) markReadIfFollowing();
	};

	useEffect(() => {
		if (switcherOpen) nodeSessions.refresh();
	}, [switcherOpen, nodeSessions.refresh]);

	const submit = async () => {
		const text = draft.trim();
		if (!text) return;
		setActionError(null);
		// 主动发言后回到最新一条，即使刚才正在往上翻历史。
		stickToBottom.current = true;
		setFollowing(true);
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
					<Text style={styles.primaryButtonText}>去待审批</Text>
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
				extra={
					<Pressable style={styles.stopButton} onPress={() => void stop()}>
						<Text style={styles.stopText}>停止</Text>
					</Pressable>
				}
			/>

			{shownStatus ? (
				<View style={styles.statusBar}>
					<View style={[styles.statusDot, { backgroundColor: statusColor(theme, tone) }]} />
					<Text style={[styles.statusText, { color: statusColor(theme, tone) }]}>
						{statusLabel(shownStatus)}
					</Text>
				</View>
			) : null}

			{error ? <Text style={styles.error}>{error}</Text> : null}
			{actionError ? <Text style={styles.error}>{actionError}</Text> : null}

			<View style={styles.messagesWrap}>
				<FlatList
					ref={listRef}
					style={styles.messages}
					contentContainerStyle={styles.messagesContent}
					data={visibleMessages}
					keyExtractor={(message) => message.id}
					renderItem={({ item }) => (
						<MessageRow message={item} fontScale={fontScale} nodeID={nodeID} />
					)}
					refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
					onContentSizeChange={handleContentSizeChange}
					onLayout={() => {
						if (!userScrolled.current) scrollToLatest(false);
					}}
					onScroll={handleScroll}
					onScrollBeginDrag={() => {
						userScrolled.current = true;
					}}
					scrollEventThrottle={16}
					ListHeaderComponent={
						permissions.length > 0 ? (
							<View style={styles.approvals}>
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
								stickToBottom.current = true;
								jumpToLatest();
								scrollToLatest(true);
							}}
						>
							<Text style={styles.newContentText}>有新内容 ·</Text>
						</Pressable>
					</View>
				) : null}
			</View>

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
		center: { flex: 1, alignItems: "center", justifyContent: "center", padding: 24, gap: 8 },
		title: { fontSize: 18, fontWeight: "600", color: colors.text },
		hint: { color: colors.textSecondary, textAlign: "center" },
		error: { color: colors.danger, paddingHorizontal: 16, paddingTop: 8 },
		stopButton: { paddingHorizontal: 6, paddingVertical: 6 },
		stopText: { fontSize: 13, color: colors.textSecondary },
		statusBar: {
			flexDirection: "row",
			alignItems: "center",
			gap: 6,
			paddingHorizontal: 16,
			paddingTop: 6,
		},
		statusDot: { width: 6, height: 6, borderRadius: 3 },
		statusText: { fontSize: 12 },
		messagesWrap: { flex: 1 },
		messages: { flex: 1 },
		messagesContent: { flexGrow: 1, justifyContent: "flex-end", padding: 16, gap: 12 },
		newContentWrap: { position: "absolute", left: 0, right: 0, bottom: 10, alignItems: "center" },
		newContent: {
			borderRadius: 14,
			paddingHorizontal: 12,
			paddingVertical: 6,
			backgroundColor: colors.action,
		},
		newContentText: { fontSize: 12, fontWeight: "600", color: colors.actionText },
		approvals: { gap: 10, marginBottom: 12 },
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
		turn: { gap: 6 },
		userTurn: { alignItems: "flex-end" },
		assistantTurn: { alignItems: "flex-start" },
		bubble: { maxWidth: "82%", borderRadius: 18, paddingHorizontal: 14, paddingVertical: 10 },
		userBubble: { backgroundColor: colors.userBubble, borderBottomRightRadius: 4 },
		assistantBubble: { backgroundColor: colors.messageBubble, borderTopLeftRadius: 4 },
		messageText: { fontSize: 15, lineHeight: 21, color: colors.text },
		userMessageText: { color: colors.userBubbleText },
		reasoningCard: {
			alignSelf: "flex-start",
			maxWidth: "88%",
			backgroundColor: colors.surfaceMuted,
			borderRadius: 12,
			padding: 10,
			gap: 4,
		},
		reasoningHeader: { flexDirection: "row", alignItems: "center", gap: 6 },
		reasoningChevron: { width: 12, fontSize: 12, color: colors.textMuted },
		reasoningLabel: { fontSize: 12, fontWeight: "600", color: colors.textSecondary },
		reasoningBody: { fontSize: 12, lineHeight: 18, color: colors.textSecondary },
		tools: { width: "100%", gap: 6 },
		noticeRow: { alignItems: "flex-start", paddingHorizontal: 4 },
		noticeError: { fontSize: 12, color: colors.danger },
		turnError: { maxWidth: "88%", paddingHorizontal: 4, fontSize: 12, color: colors.danger },
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
