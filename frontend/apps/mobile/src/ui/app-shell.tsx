import type { SessionState } from "@lework/api";
import { router } from "expo-router";
import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";

import { useOverview } from "../overview/use-overview";
import { useProjects } from "../projects/use-projects";
import { isUnread as computeUnread, watermarkKey } from "../read/read-state";
import { useReadState } from "../read/use-read-state";
import { createSecureSettingsStore } from "../settings/secure-store";
import { clearExpiredSession } from "../settings/session";
import { setSessionExpiredHandler } from "../settings/session-events";
import { useSettings } from "../settings/use-settings";
import { buildSidebarGroups } from "../sidebar/model";
import { useSidebarSessions } from "../sidebar/use-sidebar-sessions";
import { AppDrawer } from "./app-drawer";
import { AppShellContext, type AppShellValue } from "./app-shell-context";

/**
 * AppShell 持有全 App 共享的连接、项目与侧边栏数据。
 *
 * 放在根布局而不是每个页面：侧边栏是全局导航，页面切换时不能重新拉一遍
 * 项目与会话；同时 SSE 只保留总览和项目两条流，避免每页各开一条。
 */
export function AppShell({ children }: { children: ReactNode }) {
	const { settings, loading: settingsLoading, reload: reloadSettings } = useSettings();
	const settingsStore = useMemo(() => createSecureSettingsStore(), []);

	useEffect(() => {
		// 只在真的持有会话时处理：登录页与服务器校验页也会收到 401，把它们
		// 反复送回登录页会打断用户正在输入的表单。
		if (!settings?.sessionToken) return;
		return setSessionExpiredHandler(() => {
			void clearExpiredSession(settingsStore, settings)
				.catch(() => {
					// 清理失败也要回登录页：留在必然失败的界面上毫无意义。
				})
				.finally(() => {
					reloadSettings();
					router.replace("/login");
				});
		});
	}, [settings, settingsStore, reloadSettings]);

	// 未登录时不能把 settings 交给数据 hook：它们会用空令牌打一串必然 401
	// 的请求，通知器虽然忽略，但登录页会被错误横幅刷屏。
	const sessionSettings = settings?.sessionToken ? settings : null;
	const projects = useProjects(sessionSettings);
	const {
		nodes: overviewNodes,
		loading: overviewLoading,
		error: overviewError,
		refresh: refreshOverview,
	} = useOverview(sessionSettings);
	const [drawerOpen, setDrawerOpen] = useState(false);
	const [screenTitle, setScreenTitle] = useState("新任务");
	const [newTaskSeq, setNewTaskSeq] = useState(0);

	const onlineNodeIDs = useMemo(
		() => projects.nodes.filter((node) => node.connected).map((node) => node.id),
		[projects.nodes],
	);
	const sidebar = useSidebarSessions(sessionSettings, onlineNodeIDs);

	// 未读水位按「最近可见会话」剪枝：抽屉与项目页用到的键优先保留。
	const visibleReadKeys = useMemo(
		() => sidebar.sessions.map((ref) => watermarkKey(ref.nodeID, ref.session.id)),
		[sidebar.sessions],
	);
	const read = useReadState(sessionSettings, visibleReadKeys);

	// overview 是唯一的执行状态来源：它由服务端事件投影而来，客户端不重建状态机。
	const sessionState = useCallback(
		(nodeID: string, sessionID: string): SessionState | undefined => {
			const node = overviewNodes.find((candidate) => candidate.node_id === nodeID);
			return node?.sessions.find((session) => session.session_id === sessionID)?.state;
		},
		[overviewNodes],
	);

	const isUnread = useCallback(
		(updatedAt: string | undefined, nodeID: string, sessionID: string) =>
			computeUnread(updatedAt, read.watermarks[watermarkKey(nodeID, sessionID)], Date.now()),
		[read.watermarks],
	);

	const groups = useMemo(
		() =>
			buildSidebarGroups(
				projects.projects,
				sidebar.sessions,
				onlineNodeIDs,
				projects.inbox.project?.key,
				sessionState,
			),
		[projects.projects, projects.inbox.project?.key, sidebar.sessions, onlineNodeIDs, sessionState],
	);

	const pendingCount = useMemo(
		() =>
			overviewNodes.reduce(
				(total, node) =>
					total + node.sessions.reduce((sum, session) => sum + session.pending_permissions, 0),
				0,
			),
		[overviewNodes],
	);

	const openDrawer = useCallback(() => {
		setDrawerOpen(true);
		// 侧边栏是快照的展示面：每次打开都刷新，避免列出已结束的会话。
		sidebar.refresh();
	}, [sidebar]);

	const closeDrawer = useCallback(() => setDrawerOpen(false), []);

	const refreshSidebar = useCallback(() => {
		sidebar.refresh();
		projects.refresh();
	}, [sidebar, projects]);

	const startNewTask = useCallback(() => {
		setDrawerOpen(false);
		setScreenTitle("新任务");
		setNewTaskSeq((value) => value + 1);
		router.navigate("/");
	}, []);

	const value = useMemo<AppShellValue>(
		() => ({
			settings,
			settingsLoading,
			reloadSettings,
			projects,
			pendingCount,
			overviewNodes,
			overviewLoading,
			overviewError,
			refreshOverview,
			sessionState,
			watermarks: read.watermarks,
			markRead: read.markRead,
			isUnread,
			groups,
			sidebarLoading: sidebar.loading,
			refreshSidebar,
			drawerOpen,
			openDrawer,
			closeDrawer,
			screenTitle,
			setScreenTitle,
			newTaskSeq,
			startNewTask,
		}),
		[
			settings,
			settingsLoading,
			reloadSettings,
			projects,
			pendingCount,
			overviewNodes,
			overviewLoading,
			overviewError,
			refreshOverview,
			sessionState,
			read.watermarks,
			read.markRead,
			isUnread,
			groups,
			sidebar.loading,
			refreshSidebar,
			drawerOpen,
			openDrawer,
			closeDrawer,
			screenTitle,
			newTaskSeq,
			startNewTask,
		],
	);

	return (
		<AppShellContext.Provider value={value}>
			{children}
			<AppDrawer />
		</AppShellContext.Provider>
	);
}
