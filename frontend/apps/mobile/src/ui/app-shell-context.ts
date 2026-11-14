import type { OverviewNode } from "@lework/api";
import { createContext, useContext, useEffect } from "react";

import type { ProjectsState } from "../projects/use-projects";
import type { Settings } from "../settings/settings";
import type { SidebarGroup } from "../sidebar/model";

/** AppShellValue 是根布局向所有页面共享的连接与导航状态。 */
export interface AppShellValue {
	settings: Settings | null;
	settingsLoading: boolean;
	/** 设置页保存后调用，让根布局里持有的 settings 立即更新。 */
	reloadSettings: () => void;
	projects: ProjectsState;
	/** 待审批总数，供通知角标使用。 */
	pendingCount: number;
	/** 控制面总览节点；只用于诊断与待审批，不参与首页布局。 */
	overviewNodes: OverviewNode[];
	overviewLoading: boolean;
	overviewError: string | null;
	refreshOverview: () => void;
	/** 侧边栏按项目分组的会话。 */
	groups: SidebarGroup[];
	sidebarLoading: boolean;
	refreshSidebar: () => void;
	drawerOpen: boolean;
	openDrawer: () => void;
	closeDrawer: () => void;
	/** 当前页面的项目/会话名称，显示在侧边栏顶部。 */
	screenTitle: string;
	setScreenTitle: (title: string) => void;
	/** 每次点「新任务」自增，首页据此把光标放回输入框。 */
	newTaskSeq: number;
	startNewTask: () => void;
}

export const AppShellContext = createContext<AppShellValue | null>(null);

/** useAppShell 读取根布局共享状态；在 AppShell 之外使用属于接线错误。 */
export function useAppShell(): AppShellValue {
	const value = useContext(AppShellContext);
	if (!value) throw new Error("useAppShell must be used inside AppShell");
	return value;
}

/**
 * useScreenTitle 让页面把自己的标题注册到侧边栏顶部。
 *
 * 不做卸载清理：页面切换时新页面会立即覆盖，清理反而会让标题短暂
 * 跳回上一个值。根布局初始值是「新任务」，与首页一致。
 */
export function useScreenTitle(title: string) {
	const { setScreenTitle } = useAppShell();
	useEffect(() => {
		setScreenTitle(title);
	}, [title, setScreenTitle]);
}
