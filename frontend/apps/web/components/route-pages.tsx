"use client";

import {
	AutomationDetailRoute,
	AutomationRoute,
	EmptyRoute,
	NewTaskRoute,
	OrgAdminRoute,
	type ProjectTab,
	ProjectRoute,
	ProjectsHubRoute,
	SkillDetailRoute,
	SkillsRoute,
	TaskDetailRoute,
	WorkbenchRoute,
	PayrollRoute,
	projectPath,
} from "@lework/control-ui";
import { useParams, useRouter } from "next/navigation";
import { useWebNavigation } from "./LeworkShell";

// 这一层只把 Next 的 useParams / useSearchParams 结果映射成共享页面的 props，
// 页面行为、导航目标全部来自 @lework/control-ui，不再直接 import app-ui 页面。

export function WorkbenchRoutePage() {
	return <WorkbenchRoute navigation={useWebNavigation()} />;
}

export function NewTaskRoutePage() {
	return <NewTaskRoute navigation={useWebNavigation()} />;
}

export function PayrollAccountingRoutePage() {
	return <PayrollRoute navigation={useWebNavigation()} />;
}

export function ProjectRoutePage({ tab = "chat" }: { tab?: ProjectTab }) {
	const navigation = useWebNavigation();
	const router = useRouter();
	const params = useParams<{ projectId: string }>();
	const projectId = params.projectId;

	return (
		<ProjectRoute
			projectId={projectId}
			tab={tab}
			navigation={navigation}
			onTabChange={(nextTab) => {
				router.push(projectPath(projectId, nextTab));
			}}
		/>
	);
}

export function TaskDetailRoutePage() {
	const navigation = useWebNavigation();
	const params = useParams<{ projectId: string; taskId: string; sessionId: string }>();

	return (
		<TaskDetailRoute
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
	return <OrgAdminRoute section={section} navigation={useWebNavigation()} />;
}

export function SkillsRoutePage() {
	return <SkillsRoute navigation={useWebNavigation()} />;
}

export function SkillDetailRoutePage() {
	const navigation = useWebNavigation();
	const params = useParams<{ skillId: string }>();

	return <SkillDetailRoute skillId={params.skillId} navigation={navigation} />;
}

export function AutomationRoutePage() {
	return <AutomationRoute navigation={useWebNavigation()} />;
}

export function AutomationExecutionRoutePage() {
	const navigation = useWebNavigation();
	const params = useParams<{ publicId: string }>();

	return <AutomationDetailRoute publicId={params.publicId} navigation={navigation} />;
}

export function ProjectsHubRoutePage() {
	return <ProjectsHubRoute navigation={useWebNavigation()} />;
}

export function EmptyRoutePage() {
	return <EmptyRoute />;
}
