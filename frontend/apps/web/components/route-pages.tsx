"use client";

import {
	AiTeammatesView,
	AssistantListView,
	CenterCanvas,
	ProjectPage,
	ProjectsHubView,
	SkillMarketView,
	TaskDetailPage,
	WorkbenchPanel,
} from "@leros/app-ui";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useWebNavigation } from "./LerosShell";

type ProjectTab = "chat" | "tasks" | "files";

export function WorkbenchRoutePage() {
	const navigation = useWebNavigation();

	return <WorkbenchPanel navigation={navigation} />;
}

export function ChatRoutePage() {
	return <CenterCanvas />;
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
				if (nextTab === "chat") {
					navigation.goToProject(projectId);
					return;
				}
				const suffix = nextTab === "tasks" ? "tasks" : "files";
				router.push(`/projects/${projectId}/${suffix}`);
			}}
		/>
	);
}

export function TaskDetailRoutePage() {
	const navigation = useWebNavigation();
	const params = useParams<{ projectId: string; taskId: string }>();
	const searchParams = useSearchParams();

	return (
		<TaskDetailPage
			projectId={params.projectId}
			taskId={params.taskId}
			sessionId={searchParams.get("sessionId")}
			navigation={navigation}
		/>
	);
}

export function AssistantsRoutePage() {
	return <AssistantListView />;
}

export function SkillsRoutePage() {
	const navigation = useWebNavigation();

	return <SkillMarketView navigation={navigation} />;
}

export function AiTeammatesRoutePage() {
	return <AiTeammatesView />;
}

export function ProjectsHubRoutePage() {
	const navigation = useWebNavigation();

	return <ProjectsHubView navigation={navigation} />;
}

export function EmptyRoutePage() {
	return <div data-slot="empty-page" className="min-h-0 flex-1 bg-[#f7f8fd]" />;
}
