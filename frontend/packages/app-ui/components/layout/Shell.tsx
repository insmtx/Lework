"use client";

import { useLayoutStore } from "@leros/store";
import type { ReactNode } from "react";
import { AuthProvider } from "../auth";
import { AssistantListView } from "../digitalAssistant/AssistantListView";
import { CenterCanvas } from "./CenterCanvas";
import { type AppNavigation, LeftRail } from "./LeftRail";
import { ProjectPage } from "./ProjectPage";
import { TaskDetailPage } from "./TaskDetailPage";
import { WorkbenchPanel } from "./WorkbenchPanel";

export function Shell({
	logoSrc,
	navigation,
	children,
}: {
	logoSrc?: string;
	navigation?: AppNavigation;
	children?: ReactNode;
}) {
	const currentView = useLayoutStore((s) => s.currentView);

	return (
		<AuthProvider logoSrc={logoSrc}>
			<div className="leros-app-shell">
				<LeftRail logoSrc={logoSrc} navigation={navigation} />
				{children ?? (
					<>
						{currentView === "chat" && <CenterCanvas />}
						{currentView === "workbench" && <WorkbenchPanel />}
						{currentView === "tasks" && <EmptyPage />}
						{currentView === "project" && <ProjectPage />}
						{currentView === "taskDetail" && <TaskDetailPage />}
						{currentView === "digitalAssistant" && <AssistantListView />}
					</>
				)}
			</div>
		</AuthProvider>
	);
}

function EmptyPage() {
	return <div data-slot="empty-page" className="min-h-0 flex-1 bg-[#f7f8fd]" />;
}
