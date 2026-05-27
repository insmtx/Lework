"use client";

import { useLayoutStore } from "@leros/store";
import { AssistantListView } from "../digitalAssistant/AssistantListView";
import { CenterCanvas } from "./CenterCanvas";
import { LeftRail } from "./LeftRail";
import { ProjectPage } from "./ProjectPage";
import { TaskDetailPage } from "./TaskDetailPage";
import { WorkbenchPanel } from "./WorkbenchPanel";

export function Shell({ logoSrc }: { logoSrc?: string }) {
	const currentView = useLayoutStore((s) => s.currentView);

	return (
		<div className="leros-app-shell">
			<LeftRail logoSrc={logoSrc} />
			{currentView === "chat" && <CenterCanvas />}
			{currentView === "workbench" && <WorkbenchPanel />}
			{currentView === "tasks" && <EmptyPage />}
			{currentView === "project" && <ProjectPage />}
			{currentView === "taskDetail" && <TaskDetailPage />}
			{currentView === "digitalAssistant" && <AssistantListView />}
		</div>
	);
}

function EmptyPage() {
	return <div data-slot="empty-page" className="min-h-0 flex-1 bg-[#f7f8fd]" />;
}
