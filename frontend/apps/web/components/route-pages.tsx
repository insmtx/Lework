"use client";

import {
	AutomationExecutionPage,
	AutomationListView,
	OrgAdminPage,
	ProjectPage,
	ProjectsHubView,
	SkillMarketView,
	TaskDetailPage,
	WorkbenchPanel,
} from "@leros/app-ui";
import { useParams, useRouter } from "next/navigation";
import { useWebNavigation } from "./LerosShell";

type ProjectTab = "chat" | "tasks" | "files" | "activity";

function projectTabPath(projectId: string, tab: ProjectTab): string {
	if (tab === "chat") return `/projects/${projectId}`;
	if (tab === "tasks") return `/projects/${projectId}/tasks`;
	if (tab === "files") return `/projects/${projectId}/files`;
	return `/projects/${projectId}/activity`;
}

export function WorkbenchRoutePage() {
	const navigation = useWebNavigation();

	return <WorkbenchPanel navigation={navigation} />;
}

export function ProjectRoutePage({ tab = "chat" }: { tab?: ProjectTab }) {
	const navigation = useWebNavigation();
	const router = useRouter();
	const params = useParams<{ projectId: string }>();
	const projectId = params.projectId;

	return (
		<ProjectPage
			projectId={projectId}
			tab={tab}
			navigation={navigation}
			onTabChange={(nextTab) => {
				router.push(projectTabPath(projectId, nextTab));
			}}
		/>
	);
}

export function TaskDetailRoutePage() {
	const navigation = useWebNavigation();
	const params = useParams<{ projectId: string; taskId: string; sessionId: string }>();

	return (
		<TaskDetailPage
			projectId={params.projectId}
			taskId={params.taskId}
			sessionId={params.sessionId}
			navigation={navigation}
		/>
	);
}

export function OrgAdminRoutePage({
	section,
}: {
	section: "profile" | "departments" | "assistants" | "models";
}) {
	const navigation = useWebNavigation();

	return <OrgAdminPage section={section} navigation={navigation} />;
}

export function SkillsRoutePage() {
	const navigation = useWebNavigation();

	return <SkillMarketView navigation={navigation} />;
}

export function AutomationRoutePage() {
	const navigation = useWebNavigation();

	return <AutomationListView navigation={navigation} />;
}

export function AutomationExecutionRoutePage() {
	const navigation = useWebNavigation();
	const params = useParams<{ publicId: string }>();

	return <AutomationExecutionPage automationPublicId={params.publicId} navigation={navigation} />;
}

export function ProjectsHubRoutePage() {
	const navigation = useWebNavigation();

	return <ProjectsHubView navigation={navigation} />;
}

export function EmptyRoutePage() {
	return (
		<div data-slot="empty-page" className="flex min-h-0 flex-1 flex-col bg-[#f7f8fd]">
			<header className="z-10 flex h-20 shrink-0 items-center justify-end px-10" />
		</div>
	);
}
