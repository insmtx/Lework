"use client";

import {
	type AppNavigation,
	AutomationExecutionPage,
	AutomationListView,
	NewTaskPage,
	OrgAdminPage,
	PayrollAccountingPage,
	ProjectPage,
	ProjectsHubView,
	SkillDetailView,
	SkillMarketView,
	TaskDetailPage,
	WorkbenchPage,
	buildSkillNewTaskPrefill,
} from "@lework/app-ui";
import { useLayoutStore } from "@lework/store";
import { useCallback } from "react";
import { toast } from "sonner";
import type { ProjectTab } from "./routes";

/**
 * 这些包装只做「宿主参数 → app-ui 页面 props」的映射。
 *
 * 页面本身仍归 app-ui 维护，所以这里不写布局、不碰数据请求，只把宿主解析
 * 出来的动态段和导航回调传下去，Web 与桌面因此共用同一份页面行为。
 */

export function NewTaskRoute({ navigation }: { navigation: AppNavigation }) {
	return <NewTaskPage navigation={navigation} />;
}

export function WorkbenchRoute({ navigation }: { navigation: AppNavigation }) {
	return <WorkbenchPage navigation={navigation} />;
}

export function PayrollRoute({ navigation }: { navigation: AppNavigation }) {
	return <PayrollAccountingPage navigation={navigation} />;
}

export function ProjectsHubRoute({ navigation }: { navigation: AppNavigation }) {
	return <ProjectsHubView navigation={navigation} />;
}

export function ProjectRoute({
	projectId,
	tab = "chat",
	navigation,
	onTabChange,
}: {
	projectId: string;
	tab?: ProjectTab;
	navigation: AppNavigation;
	onTabChange: (tab: ProjectTab) => void;
}) {
	return (
		<ProjectPage
			projectId={projectId}
			tab={tab}
			navigation={navigation}
			onTabChange={onTabChange}
		/>
	);
}

export function TaskDetailRoute({
	projectId,
	taskId,
	sessionId,
	navigation,
}: {
	projectId: string;
	taskId: string;
	sessionId: string;
	navigation: AppNavigation;
}) {
	return (
		<TaskDetailPage
			projectId={projectId}
			taskId={taskId}
			sessionId={sessionId}
			navigation={navigation}
		/>
	);
}

export function OrgAdminRoute({
	section,
	navigation,
}: {
	section: "profile" | "departments" | "assistants" | "models";
	navigation: AppNavigation;
}) {
	return <OrgAdminPage section={section} navigation={navigation} />;
}

export function SkillsRoute({ navigation }: { navigation: AppNavigation }) {
	return <SkillMarketView navigation={navigation} />;
}

export function SkillDetailRoute({
	skillId,
	navigation,
}: {
	skillId: string;
	navigation: AppNavigation;
}) {
	const { activeProjectId, projects, setProjectRoute, setProjectComposerPrefill } = useLayoutStore(
		(s) => ({
			activeProjectId: s.activeProjectId,
			projects: s.projects,
			setProjectRoute: s.setProjectRoute,
			setProjectComposerPrefill: s.setProjectComposerPrefill,
		}),
	);

	const handleUse = useCallback(
		(nextSkillId: string, displayLabel?: string) => {
			const targetProjectId = activeProjectId ?? projects[0]?.id;
			if (!targetProjectId) {
				toast.error("请先创建或选择项目");
				return;
			}

			const prefill = buildSkillNewTaskPrefill(nextSkillId, undefined, displayLabel);
			setProjectComposerPrefill({
				projectId: targetProjectId,
				value: prefill.value,
				tokens: prefill.tokens,
			});
			setProjectRoute(targetProjectId, "chat");
			navigation.goToProject(targetProjectId);
		},
		[activeProjectId, navigation, projects, setProjectComposerPrefill, setProjectRoute],
	);

	return (
		<SkillDetailView
			skillId={skillId}
			source="official"
			onBack={() => navigation.goToRoute("skills")}
			onUse={handleUse}
		/>
	);
}

export function AutomationRoute({ navigation }: { navigation: AppNavigation }) {
	return <AutomationListView navigation={navigation} />;
}

export function AutomationDetailRoute({
	publicId,
	navigation,
}: {
	publicId: string;
	navigation: AppNavigation;
}) {
	return <AutomationExecutionPage automationPublicId={publicId} navigation={navigation} />;
}

export function EmptyRoute() {
	return (
		<div data-slot="empty-page" className="flex min-h-0 flex-1 flex-col bg-[#f7f8fd]">
			<header className="z-10 flex h-20 shrink-0 items-center justify-end px-10" />
		</div>
	);
}
