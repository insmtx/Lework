import type { Message } from "@lework/api";
import type { SubagentView, ToolBlock } from "@lework/tool-view";
import { type ComponentRef, useEffect, useMemo, useRef, useState } from "react";
import {
	ActivityIndicator,
	FlatList,
	Modal,
	type NativeScrollEvent,
	type NativeSyntheticEvent,
	Pressable,
	ScrollView,
	Text,
	View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { createClient } from "../../api/client";
import { MessageRow } from "../../session/message-row";
import { filterVisibleMessages } from "../../session/messages";
import { isNearBottom } from "../../session/scroll";
import { useSession } from "../../session/use-session";
import type { Settings } from "../../settings/settings";
import { useThemePreference } from "../../theme/theme-context";
import { useTheme } from "../../theme/use-theme";
import { useAppShell } from "../app-shell-context";
import { useToolStyles } from "./font-scale";
import { pickSubagentSession } from "./pick-subagent-session";
import { RawJson } from "./raw-json";
import { ToolBlocks } from "./tool-blocks";

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

/**
 * SubagentSheet 用全屏页承载子代理详情。
 *
 * prompt 和结果经常是整段文本，塞进气泡只能看到几行。执行中的工具记录还没
 * 有子会话 id，所以先按 description 在父会话的子会话里找，找到后直接渲染
 * 子代理自己的消息流并实时跟随；找不到时退回静态的 prompt/结果。
 * 右上角 ✕ 与系统返回键是统一的关闭方式。
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
	fontScale,
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
	fontScale: number;
	onClose: () => void;
}) {
	const styles = useToolStyles();
	const insets = useSafeAreaInsets();

	return (
		<Modal visible={visible} animationType="slide" onRequestClose={onClose}>
			<View style={[styles.sheetScreen, { paddingTop: insets.top }]}>
				<View style={styles.sheetHeader}>
					<View style={styles.sheetHeading}>
						<Text style={styles.sheetTitle}>子代理</Text>
						<Text style={styles.sheetSubtitle} numberOfLines={1}>
							{detail.agent || "agent"}
						</Text>
					</View>
					<Pressable
						style={styles.sheetCloseButton}
						hitSlop={8}
						onPress={onClose}
						accessibilityLabel="关闭子代理详情"
					>
						<Text style={styles.sheetCloseIcon}>✕</Text>
					</Pressable>
				</View>

				{visible ? (
					<SubagentBody
						detail={detail}
						tool={tool}
						status={status}
						running={running}
						blocks={blocks}
						raw={raw}
						nodeID={nodeID}
						sessionID={sessionID}
						fontScale={fontScale}
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
	nodeID,
	sessionID,
	fontScale,
	paddingBottom,
}: {
	detail: SubagentView;
	tool: string;
	status: string;
	running: boolean;
	blocks: ToolBlock[];
	raw: string;
	nodeID: string;
	sessionID: string;
	fontScale: number;
	paddingBottom: number;
}) {
	const { settings } = useAppShell();
	const [discoveredID, setDiscoveredID] = useState<string | null>(null);
	// 完成后的 metadata.sessionID 是权威值；执行中退回轮询找到的子会话。
	const childSessionID = detail.sessionID || discoveredID || "";

	// 打开新面板时清掉上一次的猜测，避免把前一个子代理的会话带过来。
	// biome-ignore lint/correctness/useExhaustiveDependencies: 只在描述变化时重置发现结果
	useEffect(() => {
		setDiscoveredID(null);
	}, [detail.sessionID, detail.description]);

	useEffect(() => {
		if (childSessionID || !settings || !sessionID) return;
		// 先用 metadata 之外的信息找一次；标题匹配不上时靠轮询等子会话创建。
		let cancelled = false;
		const client = createClient(settings);
		const load = async () => {
			try {
				const children = await client.listChildSessions(nodeID, sessionID);
				if (cancelled) return;
				const picked = pickSubagentSession(children, detail.description ?? "");
				if (picked) setDiscoveredID(picked.id);
			} catch {
				// 找不到就继续用静态内容，不因为一次查询失败打断面板。
			}
		};
		void load();
		// 执行中才持续轮询；完成且仍无匹配时说明拿不到子会话，停止请求。
		const timer = running ? setInterval(() => void load(), CHILD_POLL_MS) : null;
		return () => {
			cancelled = true;
			if (timer) clearInterval(timer);
		};
	}, [childSessionID, running, settings, nodeID, sessionID, detail.description]);

	const meta = [tool, status, detail.duration ?? "", detail.background ? "后台运行" : ""]
		.filter((part) => part !== "")
		.join(" · ");

	if (childSessionID !== "") {
		return (
			<LiveConversation
				settings={settings}
				nodeID={nodeID}
				childSessionID={childSessionID}
				fontScale={fontScale}
				meta={meta}
				description={detail.description ?? ""}
				paddingBottom={paddingBottom}
			/>
		);
	}

	return (
		<StaticDetail
			blocks={blocks}
			raw={raw}
			meta={meta}
			description={detail.description ?? ""}
			syncing={running}
			paddingBottom={paddingBottom}
		/>
	);
}

/** LiveConversation 渲染子代理自己的消息流，打开即贴底并随内容增长跟随。 */
function LiveConversation({
	settings,
	nodeID,
	childSessionID,
	fontScale,
	meta,
	description,
	paddingBottom,
}: {
	settings: Settings | null;
	nodeID: string;
	childSessionID: string;
	fontScale: number;
	meta: string;
	description: string;
	paddingBottom: number;
}) {
	const theme = useTheme();
	const styles = useToolStyles();
	const { preferences } = useThemePreference();
	const { messages, loading, setFollowing } = useSession(settings, nodeID, childSessionID);
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
		<FlatList
			ref={listRef}
			style={styles.sheetBody}
			contentContainerStyle={[styles.sheetLiveContent, { paddingBottom }]}
			data={visible}
			keyExtractor={(message) => message.id}
			// 字号与展示偏好变化时行内内容要一起重排。
			extraData={`${fontScale}:${preferences.toolDisplay}:${preferences.processDisplay}`}
			renderItem={({ item, index }) => (
				<MessageRow
					message={item}
					fontScale={fontScale}
					isLastMessage={index === visible.length - 1}
					nodeID={nodeID}
					sessionID={childSessionID}
				/>
			)}
			ListHeaderComponent={
				<View style={styles.sheetMeta}>
					<Text style={styles.sheetMetaText}>{meta}</Text>
					{description ? <Text style={styles.sheetDescription}>{description}</Text> : null}
				</View>
			}
			ListEmptyComponent={
				loading ? (
					<ActivityIndicator style={styles.sheetLoading} color={theme.colors.accent} />
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
	);
}

/** StaticDetail 是没有子会话时的兜底：直接展示 prompt、结果与原始数据。 */
function StaticDetail({
	blocks,
	raw,
	meta,
	description,
	syncing,
	paddingBottom,
}: {
	blocks: ToolBlock[];
	raw: string;
	meta: string;
	description: string;
	syncing: boolean;
	paddingBottom: number;
}) {
	const styles = useToolStyles();
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
			<View style={styles.sheetMeta}>
				<Text style={styles.sheetMetaText}>{meta}</Text>
				{description ? <Text style={styles.sheetDescription}>{description}</Text> : null}
				{syncing ? <Text style={styles.sheetMetaText}>正在同步子代理会话…</Text> : null}
			</View>

			<ToolBlocks blocks={blocks} />
			<RawJson text={raw} />
		</ScrollView>
	);
}
