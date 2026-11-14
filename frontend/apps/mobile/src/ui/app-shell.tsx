import { router } from "expo-router";
import { type ReactNode, useCallback, useMemo, useState } from "react";

import { useOverview } from "../overview/use-overview";
import { useProjects } from "../projects/use-projects";
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
	const projects = useProjects(settings);
	const {
		nodes: overviewNodes,
		loading: overviewLoading,
		error: overviewError,
		refresh: refreshOverview,
	} = useOverview(settings);
	const [drawerOpen, setDrawerOpen] = useState(false);
	const [screenTitle, setScreenTitle] = useState("新任务");
	const [newTaskSeq, setNewTaskSeq] = useState(0);

	const onlineNodeIDs = useMemo(
		() => projects.nodes.filter((node) => node.connected).map((node) => node.id),
		[projects.nodes],
	);
	const sidebar = useSidebarSessions(settings, onlineNodeIDs);
	const groups = useMemo(
		() =>
			buildSidebarGroups(
				projects.projects,
				sidebar.sessions,
				onlineNodeIDs,
				projects.inbox.project?.key,
			),
		[projects.projects, projects.inbox.project?.key, sidebar.sessions, onlineNodeIDs],
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
