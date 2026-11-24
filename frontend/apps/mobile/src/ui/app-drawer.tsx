import type { SessionState } from "@lework/api";
import { router } from "expo-router";
import { useEffect, useMemo, useRef, useState } from "react";
import {
	Animated,
	Modal,
	Pressable,
	ScrollView,
	StyleSheet,
	Text,
	useWindowDimensions,
	View,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { projectRoute, sessionRoute } from "../config";
import { nodeColorSwatch } from "../nodes/colors";
import { projectDisplayName } from "../projects/model";
import { formatRelativeTime, RECENT_SESSION_LIMIT, type SidebarGroup } from "../sidebar/model";
import type { Theme } from "../theme/theme";
import { useThemedStyles } from "../theme/use-theme";
import { useAppShell } from "./app-shell-context";
import { BrandMark } from "./brand";
import { SessionStatusMark, UnreadDot } from "./session-status-mark";
import { BellButton } from "./top-bar";

function ProjectGroup({
	group,
	nodeColors,
	stateOf,
	onOpenProject,
	onOpenSession,
}: {
	group: SidebarGroup;
	nodeColors: Record<string, string>;
	stateOf: (nodeID: string, sessionID: string) => SessionState | undefined;
	onOpenProject: () => void;
	onOpenSession: (nodeID: string, sessionID: string) => void;
}) {
	const styles = useThemedStyles(createStyles);
	const { isUnread } = useAppShell();

	return (
		<View style={styles.group}>
			<Pressable style={styles.projectRow} onPress={onOpenProject}>
				<Text style={styles.projectName} numberOfLines={1}>
					{projectDisplayName(group.project)}
				</Text>
			</Pressable>

			{group.recent.map((row) => {
				const state = stateOf(row.nodeID, row.session.id);
				const unread = isUnread(row.session.updatedAt, row.nodeID, row.session.id);

				return (
					<Pressable
						key={`${row.nodeID}:${row.session.id}`}
						style={[
							styles.sessionRow,
							{ borderLeftColor: nodeColorSwatch(nodeColors[row.nodeID]) ?? "transparent" },
							state === "waiting_permission" && styles.sessionRowWaiting,
						]}
						onPress={() => onOpenSession(row.nodeID, row.session.id)}
					>
						<SessionStatusMark state={state} color={nodeColorSwatch(nodeColors[row.nodeID])} />
						<Text style={styles.sessionTitle} numberOfLines={1}>
							{row.session.title || row.session.id}
						</Text>
						<Text style={styles.sessionTime}>{formatRelativeTime(row.session.updatedAt)}</Text>
						{unread ? <UnreadDot /> : null}
					</Pressable>
				);
			})}

			{group.total > RECENT_SESSION_LIMIT ? (
				<Pressable style={styles.moreRow} onPress={onOpenProject}>
					<Text style={styles.moreText}>全部会话 ›</Text>
				</Pressable>
			) : null}
		</View>
	);
}

/**
 * AppDrawer 是左侧滑出的主导航（豆包式），而不是项目树。
 *
 * 顶部是当前项目/会话名与通知；中间是「新任务 / 我的电脑 / 查看全部」
 * 和按项目分组的最近会话；底部只留用户与设置。没有的能力（定时任务、插件）不占位。
 */
export function AppDrawer() {
	const {
		settings,
		projects,
		groups,
		sidebarLoading,
		drawerOpen,
		closeDrawer,
		screenTitle,
		startNewTask,
		sessionState,
	} = useAppShell();
	const insets = useSafeAreaInsets();
	const styles = useThemedStyles(createStyles);
	const { width } = useWindowDimensions();
	const panelWidth = Math.min(Math.round(width * 0.88), 360);
	const progress = useRef(new Animated.Value(0)).current;
	const [userOpen, setUserOpen] = useState(false);
	const nodeColors = useMemo(
		() => Object.fromEntries(projects.nodes.map((node) => [node.id, node.color ?? ""])),
		[projects.nodes],
	);

	useEffect(() => {
		if (!drawerOpen) return;
		progress.setValue(0);
		Animated.timing(progress, {
			toValue: 1,
			duration: 220,
			useNativeDriver: true,
		}).start();
	}, [drawerOpen, progress]);

	useEffect(() => {
		if (!drawerOpen) setUserOpen(false);
	}, [drawerOpen]);

	const go = (route: string) => {
		closeDrawer();
		router.push(route);
	};

	const openProject = (key: string) => {
		closeDrawer();
		router.push(projectRoute(key));
	};

	const openSession = (nodeID: string, sessionID: string) => {
		closeDrawer();
		router.push(sessionRoute(nodeID, sessionID));
	};

	return (
		<Modal
			transparent
			statusBarTranslucent
			visible={drawerOpen}
			animationType="none"
			onRequestClose={closeDrawer}
		>
			<View style={styles.root}>
				<Animated.View style={[styles.backdrop, { opacity: progress }]}>
					<Pressable style={StyleSheet.absoluteFill} onPress={closeDrawer} />
				</Animated.View>

				<Animated.View
					style={[
						styles.panel,
						{
							width: panelWidth,
							paddingTop: insets.top + 6,
							paddingBottom: Math.max(insets.bottom, 10),
							transform: [
								{
									translateX: progress.interpolate({
										inputRange: [0, 1],
										outputRange: [-panelWidth, 0],
									}),
								},
							],
						},
					]}
				>
					<View style={styles.topRow}>
						<Pressable
							accessibilityLabel="关闭菜单"
							hitSlop={12}
							style={styles.menuButton}
							onPress={closeDrawer}
						>
							<Text style={styles.menuText}>☰</Text>
						</Pressable>
						<BrandMark size={22} />
						<Text style={styles.topTitle} numberOfLines={1}>
							{screenTitle}
						</Text>
						<BellButton
							onPress={() => {
								closeDrawer();
								router.push("/pending");
							}}
						/>
					</View>

					<ScrollView
						style={styles.scroll}
						contentContainerStyle={styles.scrollContent}
						showsVerticalScrollIndicator={false}
					>
						<Pressable style={styles.actionRow} onPress={startNewTask}>
							<Text style={styles.actionIcon}>＋</Text>
							<Text style={styles.actionLabel}>新任务</Text>
						</Pressable>
						<Pressable style={styles.actionRow} onPress={() => go("/nodes")}>
							<Text style={styles.actionIcon}>▣</Text>
							<Text style={styles.actionLabel}>我的电脑</Text>
						</Pressable>
						<Pressable style={styles.actionRow} onPress={() => go("/projects")}>
							<Text style={styles.actionIcon}>▤</Text>
							<Text style={styles.actionLabel}>查看全部</Text>
						</Pressable>

						<View style={styles.separator} />

						<Text style={styles.sectionTitle}>项目</Text>
						{groups.length === 0 ? (
							<Text style={styles.empty}>
								{sidebarLoading ? "正在加载项目…" : "还没有项目，直接发个任务试试。"}
							</Text>
						) : (
							groups.map((group) => (
								<ProjectGroup
									key={group.project.key}
									group={group}
									nodeColors={nodeColors}
									stateOf={sessionState}
									onOpenProject={() => openProject(group.project.key)}
									onOpenSession={openSession}
								/>
							))
						)}
					</ScrollView>

					<View style={styles.separator} />

					<View style={styles.bottomRow}>
						<Pressable style={styles.bottomButton} onPress={() => setUserOpen((open) => !open)}>
							<Text style={styles.bottomLabel}>用户</Text>
						</Pressable>
						<Pressable style={styles.bottomButton} onPress={() => go("/settings")}>
							<Text style={styles.bottomLabel}>设置</Text>
						</Pressable>
					</View>

					{userOpen ? (
						<View style={styles.userCard}>
							<Text style={styles.userLine} numberOfLines={2}>
								{settings ? settings.baseUrl : "尚未连接控制面"}
							</Text>
							<Text style={styles.userMeta}>
								{settings
									? projects.connected
										? "事件流已连接"
										: "事件流重连中"
									: "在设置里填写地址与令牌"}
							</Text>
						</View>
					) : null}
				</Animated.View>
			</View>
		</Modal>
	);
}

function createStyles(theme: Theme) {
	const { colors, radii } = theme;
	return StyleSheet.create({
		root: { flex: 1 },
		backdrop: {
			position: "absolute",
			top: 0,
			right: 0,
			bottom: 0,
			left: 0,
			backgroundColor: colors.backdrop,
		},
		panel: {
			position: "absolute",
			top: 0,
			bottom: 0,
			left: 0,
			backgroundColor: colors.sidebarBackground,
			borderRightWidth: 1,
			borderRightColor: colors.sidebarBorder,
			paddingHorizontal: 14,
		},
		topRow: { flexDirection: "row", alignItems: "center", gap: 8, paddingBottom: 10 },
		menuButton: { width: 32, height: 32, alignItems: "center", justifyContent: "center" },
		menuText: { fontSize: 20, color: colors.text },
		topTitle: { flex: 1, fontSize: 15, fontWeight: "600", color: colors.text },
		scroll: { flex: 1 },
		scrollContent: { paddingBottom: 8 },
		actionRow: {
			flexDirection: "row",
			alignItems: "center",
			gap: 12,
			paddingVertical: 11,
			paddingHorizontal: 4,
		},
		actionIcon: { width: 20, textAlign: "center", fontSize: 16, color: colors.textStrong },
		actionLabel: { fontSize: 15, color: colors.text },
		separator: { height: 1, backgroundColor: colors.borderSubtle, marginVertical: 8 },
		sectionTitle: {
			fontSize: 12,
			fontWeight: "600",
			color: colors.textMuted,
			paddingHorizontal: 4,
			paddingTop: 2,
			paddingBottom: 4,
		},
		empty: { fontSize: 13, color: colors.textMuted, paddingHorizontal: 4, paddingVertical: 10 },
		group: { paddingBottom: 8 },
		projectRow: { paddingHorizontal: 4, paddingTop: 8, paddingBottom: 4 },
		projectName: { fontSize: 14, fontWeight: "600", color: colors.text },
		sessionRow: {
			flexDirection: "row",
			alignItems: "center",
			gap: 8,
			// 左侧 3px 固定留白，节点色缺省时也不让文字左右跳。
			borderLeftWidth: 3,
			paddingLeft: 13,
			paddingRight: 4,
			paddingVertical: 7,
		},
		sessionTitle: { flex: 1, fontSize: 14, color: colors.textStrong },
		sessionRowWaiting: { backgroundColor: colors.warningSurface, borderRadius: radii.sm },
		sessionTime: { fontSize: 11, color: colors.textMuted },
		moreRow: { paddingLeft: 16, paddingVertical: 6 },
		moreText: { fontSize: 13, color: colors.accent },
		bottomRow: { flexDirection: "row", justifyContent: "space-between", paddingHorizontal: 4 },
		bottomButton: { paddingVertical: 10 },
		bottomLabel: { fontSize: 15, color: colors.text },
		userCard: {
			marginHorizontal: 4,
			marginBottom: 4,
			padding: 10,
			borderRadius: radii.md,
			backgroundColor: colors.surfaceMuted,
			gap: 2,
		},
		userLine: { fontSize: 12, color: colors.textStrong },
		userMeta: { fontSize: 11, color: colors.textMuted },
	});
}
