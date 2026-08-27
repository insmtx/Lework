"use client";

import {
	useAuthStore,
	useChatStore,
	useGlobalConfigStore,
	useLayoutStore,
	usePermissionStore,
} from "@leros/store";
import { type ReactNode, useEffect, useState } from "react";
import { AuthProvider } from "../auth";
import { OrgAdminPage } from "../org-admin/OrgAdminPage";
import { PermissionDeniedListener } from "../permission/PermissionDeniedListener";
import { FrontendEventTracker } from "../telemetry/FrontendEventTracker";
import { FilePreviewHost } from "./FilePreviewHost";
import { type AppNavigation, LeftRail } from "./LeftRail";
import { NewTaskPage } from "./NewTaskPage";
import { ProjectPage } from "./ProjectPage";
import { TaskDetailPage } from "./TaskDetailPage";
import { WorkbenchPage } from "./WorkbenchPage";

export function Shell({
	logoSrc,
	navigation,
	children,
}: {
	logoSrc?: string;
	navigation?: AppNavigation;
	children?: ReactNode;
}) {
	const [isClientMounted, setIsClientMounted] = useState(false);
	const currentView = useLayoutStore((s) => s.currentView);
	const activeTaskDetailSessionId = useLayoutStore((s) => s.activeTaskDetailSessionId);
	const { startGlobalEvents, stopGlobalEvents } = useChatStore((s) => s);
	const orgId = useAuthStore((s) => s.authUser?.currentOrg?.id);
	const invalidateAll = usePermissionStore((s) => s.invalidateAll);
	const fetchGlobalConfig = useGlobalConfigStore((s) => s.fetchGlobalConfig);

	useEffect(() => {
		// 中文注释：全局配置不依赖登录态，应用启动时统一加载，刷新页面时重新获取服务端版本信息。
		void fetchGlobalConfig();
	}, [fetchGlobalConfig]);

	useEffect(() => {
		// 中文注释：客户端挂载后再渲染工作台，确保侧边栏本地偏好不会参与 SSR hydration。
		setIsClientMounted(true);
	}, []);

	useEffect(() => {
		invalidateAll();
	}, [invalidateAll, orgId]);

	useEffect(() => {
		if (orgId == null) {
			stopGlobalEvents();
			return;
		}
		// 中文注释：组织切换后 JWT org 已变，必须重连 GlobalEvents 才能收到新 org 的 message.created。
		stopGlobalEvents();
		void startGlobalEvents();
		return () => {
			stopGlobalEvents();
		};
	}, [orgId, startGlobalEvents, stopGlobalEvents]);

	if (!isClientMounted) {
		return <div className="leros-app-shell" aria-hidden="true" />;
	}

	return (
		<AuthProvider logoSrc={logoSrc}>
			<PermissionDeniedListener />
			<FrontendEventTracker currentPath={navigation?.currentPath} />
			<div className="leros-app-shell">
				<LeftRail logoSrc={logoSrc} navigation={navigation} />
				{children ?? (
					<>
						{currentView === "chat" && <NewTaskPage />}
						{currentView === "workbench" && <WorkbenchPage />}
						{currentView === "tasks" && <EmptyPage />}
						{currentView === "project" && <ProjectPage />}
						{currentView === "taskDetail" && activeTaskDetailSessionId && (
							<TaskDetailPage sessionId={activeTaskDetailSessionId} />
						)}
						{currentView === "orgProfile" && <OrgAdminPage section="profile" />}
						{currentView === "orgDepartments" && <OrgAdminPage section="departments" />}
						{currentView === "orgAssistants" && <OrgAdminPage section="assistants" />}
						{currentView === "orgModels" && <OrgAdminPage section="models" />}
					</>
				)}
			</div>
			<FilePreviewHost />
		</AuthProvider>
	);
}

function EmptyPage() {
	return <div data-slot="empty-page" className="min-h-0 flex-1 bg-[#f7f8fd]" />;
}
