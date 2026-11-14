import { router } from "expo-router";
import { useEffect, useRef, useState } from "react";
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
import { projectDisplayName } from "../projects/model";
import { formatRelativeTime, RECENT_SESSION_LIMIT, type SidebarGroup } from "../sidebar/model";
import { useAppShell } from "./app-shell-context";
import { BellButton } from "./top-bar";

function ProjectGroup({
	group,
	onOpenProject,
	onOpenSession,
}: {
	group: SidebarGroup;
	onOpenProject: () => void;
	onOpenSession: (nodeID: string, sessionID: string) => void;
}) {
	return (
		<View style={styles.group}>
			<Pressable style={styles.projectRow} onPress={onOpenProject}>
				<Text style={styles.projectName} numberOfLines={1}>
					{projectDisplayName(group.project)}
				</Text>
			</Pressable>

			{group.recent.map((row) => (
				<Pressable
					key={`${row.nodeID}:${row.session.id}`}
					style={styles.sessionRow}
					onPress={() => onOpenSession(row.nodeID, row.session.id)}
				>
					<Text style={styles.sessionTitle} numberOfLines={1}>
						{row.session.title || row.session.id}
					</Text>
					<Text style={styles.sessionTime}>{formatRelativeTime(row.session.updatedAt)}</Text>
				</Pressable>
			))}

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
 * 顶部是当前项目/会话名与通知；中间是「新任务 / 查看全部」和按项目分组
 * 的最近会话；底部只留用户与设置。没有的能力（定时任务、插件）不占位。
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
	} = useAppShell();
	const insets = useSafeAreaInsets();
	const { width } = useWindowDimensions();
	const panelWidth = Math.min(Math.round(width * 0.88), 360);
	const progress = useRef(new Animated.Value(0)).current;
	const [userOpen, setUserOpen] = useState(false);

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

const styles = StyleSheet.create({
	root: { flex: 1 },
	backdrop: {
		position: "absolute",
		top: 0,
		right: 0,
		bottom: 0,
		left: 0,
		backgroundColor: "#0f172a",
	},
	panel: {
		position: "absolute",
		top: 0,
		bottom: 0,
		left: 0,
		backgroundColor: "#fff",
		paddingHorizontal: 14,
	},
	topRow: { flexDirection: "row", alignItems: "center", gap: 8, paddingBottom: 10 },
	menuButton: { width: 32, height: 32, alignItems: "center", justifyContent: "center" },
	menuText: { fontSize: 20, color: "#0f172a" },
	topTitle: { flex: 1, fontSize: 15, fontWeight: "600", color: "#0f172a" },
	scroll: { flex: 1 },
	scrollContent: { paddingBottom: 8 },
	actionRow: {
		flexDirection: "row",
		alignItems: "center",
		gap: 12,
		paddingVertical: 11,
		paddingHorizontal: 4,
	},
	actionIcon: { width: 20, textAlign: "center", fontSize: 16, color: "#334155" },
	actionLabel: { fontSize: 15, color: "#0f172a" },
	separator: { height: 1, backgroundColor: "#eef2f7", marginVertical: 8 },
	sectionTitle: {
		fontSize: 12,
		fontWeight: "600",
		color: "#94a3b8",
		paddingHorizontal: 4,
		paddingTop: 2,
		paddingBottom: 4,
	},
	empty: { fontSize: 13, color: "#94a3b8", paddingHorizontal: 4, paddingVertical: 10 },
	group: { paddingBottom: 8 },
	projectRow: { paddingHorizontal: 4, paddingTop: 8, paddingBottom: 4 },
	projectName: { fontSize: 14, fontWeight: "600", color: "#0f172a" },
	sessionRow: {
		flexDirection: "row",
		alignItems: "center",
		gap: 8,
		paddingLeft: 16,
		paddingRight: 4,
		paddingVertical: 7,
	},
	sessionTitle: { flex: 1, fontSize: 14, color: "#334155" },
	sessionTime: { fontSize: 11, color: "#94a3b8" },
	moreRow: { paddingLeft: 16, paddingVertical: 6 },
	moreText: { fontSize: 13, color: "#2563eb" },
	bottomRow: { flexDirection: "row", justifyContent: "space-between", paddingHorizontal: 4 },
	bottomButton: { paddingVertical: 10 },
	bottomLabel: { fontSize: 15, color: "#0f172a" },
	userCard: {
		marginHorizontal: 4,
		marginBottom: 4,
		padding: 10,
		borderRadius: 10,
		backgroundColor: "#f8fafc",
		gap: 2,
	},
	userLine: { fontSize: 12, color: "#334155" },
	userMeta: { fontSize: 11, color: "#94a3b8" },
});
