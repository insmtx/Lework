import type { Message, Session, SessionState } from "@lework/api";
import type { SubagentView, ToolBlock } from "@lework/tool-view";
import { type ComponentRef, useEffect, useMemo, useRef, useState } from "react";
import {
	FlatList,
	Modal,
	type NativeScrollEvent,
	type NativeSyntheticEvent,
	Pressable,
	RefreshControl,
	ScrollView,
	Text,
	View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { createClient } from "../../api/client";
import { MarkdownBody } from "../../session/markdown";
import { MessageRow } from "../../session/message-row";
import { filterVisibleMessages } from "../../session/messages";
import { isNearBottom } from "../../session/scroll";
import { statusColor, statusLabel, statusTone } from "../../session/status";
import { useSession } from "../../session/use-session";
import { useThemePreference } from "../../theme/theme-context";
import { useTheme } from "../../theme/use-theme";
import { useAppShell } from "../app-shell-context";
import { BrandSpinner } from "../loading";
import { pickSubagentSession } from "./pick-subagent-session";
import { RawJson } from "./raw-json";
import { ToolBlocks } from "./tool-blocks";
import { useToolStyles } from "./use-tool-styles";

// 子会话可能在打开面板后才创建（执行中），轮询间隔够跟上首个会话又不过度打服务端。
const CHILD_POLL_MS = 2_000;

/** nearBottom 用同一套阈值判断用户是否还贴着底部。 */
function nearBottom(event: NativeSyntheticEvent<NativeScrollEvent>): boolean {
	const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
	return isNearBottom({
		offsetY: contentOffset.y,
		contentHeight: contentSize.height,
		viewportHeight: layoutMeasurement.height,
	});
}

/** toolSessionState 把工具状态翻译成会话状态，接不上子会话时也能显示状态条。 */
function toolSessionState(running: boolean, status: string): SessionState | null {
	if (status === "error") return "error";
	if (running || status === "running" || status === "pending" || status === "streaming") {
		return "running";
	}
	return status === "" ? null : "idle";
}

/**
 * SubagentSheet 用全屏页承载子代理详情。
 *
 * 顶栏展示真实子会话标题与状态，正文优先渲染子代理自己的消息流；只有实在
 * 找不到子会话时才退回「任务说明 / 执行结果」的静态内容。归属判断优先用
 * 工具完成后的 metadata.sessionID，执行中再用 running 子会话兜底。
 */
export function SubagentSheet({
	visible,
	detail,
	tool,
	status,
	running,
	blocks,
	raw,
	nodeID,
	sessionID,
	onClose,
}: {
	visible: boolean;
	detail: SubagentView;
	tool: string;
	status: string;
	/** 工具当前是否仍在执行；执行中才持续轮询子会话。 */
	running: boolean;
	blocks: ToolBlock[];
	raw: string;
	nodeID: string;
	/** 父会话 id：用于查这次调用创建的子会话。 */
	sessionID: string;
	onClose: () => void;
}) {
	const styles = useToolStyles();
	const insets = useSafeAreaInsets();
	const { settings } = useAppShell();
	const [child, setChild] = useState<Session | null>(null);

	// 切换到另一次 subagent 调用时清掉上一次的猜测，避免张冠李戴。
	// biome-ignore lint/correctness/useExhaustiveDependencies: 只在归属线索变化时重置
	useEffect(() => {
		setChild(null);
	}, [detail.sessionID, detail.description]);

	useEffect(() => {
		if (!visible || !settings || !sessionID) return;
		let cancelled = false;
		const client = createClient(settings);
		const load = async () => {
			try {
				const children = await client.listChildSessions(nodeID, sessionID);
				if (cancelled) return;
				const picked = detail.sessionID
					? (children.find((item) => item.id === detail.sessionID) ?? { id: detail.sessionID })
					: pickSubagentSession(children, {
							description: detail.description ?? "",
							running,
						});
				if (!picked) return;
				setChild((previous) => {
					if (
						previous &&
						previous.id === picked.id &&
						previous.title === picked.title &&
						previous.running === picked.running &&
						previous.updatedAt === picked.updatedAt
					) {
						return previous;
					}
					return picked;
				});
			} catch {
				// 查询失败时保留已有结果；静态兜底仍可读，不因为一次失败打断面板。
			}
		};
		void load();
		// 执行中持续轮询，等子会话创建、标题变化或结束；完成后不再定时请求。
		const timer = running ? setInterval(() => void load(), CHILD_POLL_MS) : null;
		return () => {
			cancelled = true;
			if (timer) clearInterval(timer);
		};
	}, [visible, settings, nodeID, sessionID, detail.sessionID, detail.description, running]);

	const title = child?.title || detail.description || "子代理";

	return (
		<Modal visible={visible} animationType="slide" onRequestClose={onClose}>
			<View style={[styles.sheetScreen, { paddingTop: insets.top }]}>
				<View style={styles.sheetHeader}>
					<Pressable
						style={styles.sheetBackButton}
						hitSlop={8}
						onPress={onClose}
						accessibilityLabel="返回"
					>
						<Text style={styles.sheetBackIcon}>‹</Text>
					</Pressable>
					<View style={styles.sheetHeading}>
						<Text style={styles.sheetTitle} numberOfLines={1}>
							{title}
						</Text>
						<Text style={styles.sheetSubtitle} numberOfLines={1}>
							子代理 · {detail.agent || tool}
						</Text>
					</View>
					<View style={styles.sheetSpacer} />
				</View>

				{visible ? (
					<SubagentBody
						detail={detail}
						tool={tool}
						status={status}
						running={running}
						blocks={blocks}
						raw={raw}
						child={child}
						title={title}
						nodeID={nodeID}
						paddingBottom={insets.bottom + 24}
					/>
				) : null}
			</View>
		</Modal>
	);
}

function SubagentBody({
	detail,
	tool,
	status,
	running,
	blocks,
	raw,
	child,
	title,
	nodeID,
	paddingBottom,
}: {
	detail: SubagentView;
	tool: string;
	status: string;
	running: boolean;
	blocks: ToolBlock[];
	raw: string;
	child: Session | null;
	title: string;
	nodeID: string;
	paddingBottom: number;
}) {
	const theme = useTheme();
	const styles = useToolStyles();
	const { settings } = useAppShell();
	// 完成后的 metadata.sessionID 是权威值；执行中用轮询找到的子会话。
	const childSessionID = detail.sessionID || child?.id || "";
	const session = useSession(settings, nodeID, childSessionID);

	const fallbackState = toolSessionState(running, status);
	const state = session.state ?? (child?.running ? "running" : null) ?? fallbackState;

	// 「执行完成」只短暂显示，常驻会把已完成误读成仍在跑（与会话页一致）。
	const [idleHidden, setIdleHidden] = useState(false);
	useEffect(() => {
		setIdleHidden(false);
		if (state !== "idle") return;
		const timer = setTimeout(() => setIdleHidden(true), 5_000);
		return () => clearTimeout(timer);
	}, [state]);
	const shownState = idleHidden ? null : state;
	const tone = statusTone(shownState);
	const statusText = statusLabel(shownState);

	const meta = [tool, detail.duration ?? "", detail.background ? "后台运行" : ""]
		.filter((part) => part !== "")
		.join(" · ");

	return (
		<View style={styles.sheetBody}>
			{statusText ? (
				<View style={styles.sheetStatusBar}>
					<View style={[styles.sheetStatusDot, { backgroundColor: statusColor(theme, tone) }]} />
					<Text style={[styles.sheetStatusText, { color: statusColor(theme, tone) }]}>
						{statusText}
					</Text>
				</View>
			) : null}

			{childSessionID !== "" ? (
				<LiveConversation
					messages={session.messages}
					loading={session.loading}
					refresh={session.refresh}
					hasNewBelow={session.hasNewBelow}
					setFollowing={session.setFollowing}
					jumpToLatest={session.jumpToLatest}
					nodeID={nodeID}
					childSessionID={childSessionID}
					title={title}
					description={detail.description ?? ""}
					meta={meta}
					paddingBottom={paddingBottom}
				/>
			) : (
				<StaticDetail
					detail={detail}
					blocks={blocks}
					raw={raw}
					meta={meta}
					syncing={running}
					paddingBottom={paddingBottom}
				/>
			)}
		</View>
	);
}

/** LiveConversation 渲染子代理自己的消息流，打开即贴底并随内容增长跟随。 */
function LiveConversation({
	messages,
	loading,
	refresh,
	hasNewBelow,
	setFollowing,
	jumpToLatest,
	nodeID,
	childSessionID,
	title,
	description,
	meta,
	paddingBottom,
}: {
	messages: Message[];
	loading: boolean;
	refresh: () => void;
	hasNewBelow: boolean;
	setFollowing: (following: boolean) => void;
	jumpToLatest: () => void;
	nodeID: string;
	childSessionID: string;
	title: string;
	description: string;
	meta: string;
	paddingBottom: number;
}) {
	const styles = useToolStyles();
	const { preferences } = useThemePreference();
	const visible = useMemo(
		() => filterVisibleMessages(messages, preferences),
		[messages, preferences],
	);

	const listRef = useRef<FlatList<Message>>(null);
	const userScrolled = useRef(false);
	const stickToBottom = useRef(true);

	const scrollToLatest = (animated: boolean) => {
		if (visible.length === 0) return;
		listRef.current?.scrollToEnd({ animated });
	};

	// 打开时直接落到最底；之后只在用户本来就贴底时跟随，向上翻历史保持原位。
	const handleContentSizeChange = () => {
		if (visible.length === 0) return;
		if (userScrolled.current && !stickToBottom.current) return;
		scrollToLatest(userScrolled.current);
		if (!userScrolled.current) {
			requestAnimationFrame(() => scrollToLatest(false));
		}
	};

	return (
		<View style={styles.sheetBody}>
			<FlatList
				ref={listRef}
				style={styles.sheetBody}
				contentContainerStyle={[styles.sheetLiveContent, { paddingBottom }]}
				data={visible}
				keyExtractor={(message) => message.id}
				// 展示偏好变化时行内内容要一起重排；字号通过主题样式与 context 自动更新。
				extraData={`${preferences.toolDisplay}:${preferences.processDisplay}`}
				renderItem={({ item, index }) => (
					<MessageRow
						message={item}
						isLastMessage={index === visible.length - 1}
						nodeID={nodeID}
						sessionID={childSessionID}
					/>
				)}
				refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}
				ListHeaderComponent={
					<View style={styles.sheetMeta}>
						{meta ? <Text style={styles.sheetMetaText}>{meta}</Text> : null}
						{description && description !== title ? (
							<Text style={styles.sheetDescription}>{description}</Text>
						) : null}
					</View>
				}
				ListEmptyComponent={
					loading ? (
						<BrandSpinner size={20} style={styles.sheetLoading} />
					) : (
						<Text style={styles.muted}>子代理还没有输出。</Text>
					)
				}
				onContentSizeChange={handleContentSizeChange}
				onLayout={() => {
					if (!userScrolled.current) scrollToLatest(false);
				}}
				onScroll={(event) => {
					stickToBottom.current = nearBottom(event);
					setFollowing(stickToBottom.current);
				}}
				onScrollBeginDrag={() => {
					userScrolled.current = true;
				}}
				scrollEventThrottle={16}
			/>

			{hasNewBelow ? (
				<View style={styles.sheetNewContentWrap} pointerEvents="box-none">
					<Pressable
						style={styles.sheetNewContent}
						onPress={() => {
							stickToBottom.current = true;
							jumpToLatest();
							scrollToLatest(true);
						}}
					>
						<Text style={styles.sheetNewContentText}>有新内容 ·</Text>
					</Pressable>
				</View>
			) : null}
		</View>
	);
}

/** StaticDetail 是没有子会话时的兜底：按任务说明与执行结果分区，而不是直接甩 JSON。 */
function StaticDetail({
	detail,
	blocks,
	raw,
	meta,
	syncing,
	paddingBottom,
}: {
	detail: SubagentView;
	blocks: ToolBlock[];
	raw: string;
	meta: string;
	syncing: boolean;
	paddingBottom: number;
}) {
	const styles = useToolStyles();
	const { scheme } = useThemePreference();
	const scrollRef = useRef<ComponentRef<typeof ScrollView>>(null);
	const userScrolled = useRef(false);
	const stickToBottom = useRef(true);

	const scrollToLatest = (animated: boolean) => {
		scrollRef.current?.scrollToEnd({ animated });
	};

	const handleContentSizeChange = () => {
		if (userScrolled.current && !stickToBottom.current) return;
		scrollToLatest(userScrolled.current);
		if (!userScrolled.current) {
			requestAnimationFrame(() => scrollToLatest(false));
		}
	};

	const errors = blocks.filter(
		(block): block is Extract<ToolBlock, { kind: "text" }> =>
			block.kind === "text" && block.tone === "error",
	);

	return (
		<ScrollView
			ref={scrollRef}
			style={styles.sheetBody}
			contentContainerStyle={[styles.sheetContent, { paddingBottom }]}
			onContentSizeChange={handleContentSizeChange}
			onLayout={() => {
				if (!userScrolled.current) scrollToLatest(false);
			}}
			onScroll={(event) => {
				stickToBottom.current = nearBottom(event);
			}}
			onScrollBeginDrag={() => {
				userScrolled.current = true;
			}}
			scrollEventThrottle={16}
		>
			{meta ? (
				<View style={styles.sheetMeta}>
					<Text style={styles.sheetMetaText}>{meta}</Text>
				</View>
			) : null}

			<View style={styles.sheetSection}>
				<Text style={styles.sheetSectionTitle}>任务说明</Text>
				{detail.description ? (
					<Text style={styles.sheetSectionText}>{detail.description}</Text>
				) : null}
				{detail.prompt && detail.prompt !== detail.description ? (
					<MarkdownBody content={detail.prompt} scheme={scheme} />
				) : null}
				{!detail.description && !detail.prompt ? (
					<Text style={styles.muted}>暂无任务说明。</Text>
				) : null}
			</View>

			<View style={styles.sheetSection}>
				<Text style={styles.sheetSectionTitle}>执行结果</Text>
				{detail.result ? (
					<MarkdownBody content={detail.result} scheme={scheme} />
				) : (
					<Text style={styles.muted}>{syncing ? "正在同步子代理会话…" : "尚未产生结果。"}</Text>
				)}
			</View>

			{errors.length > 0 ? <ToolBlocks blocks={errors} /> : null}
			<RawJson text={raw} />
		</ScrollView>
	);
}
