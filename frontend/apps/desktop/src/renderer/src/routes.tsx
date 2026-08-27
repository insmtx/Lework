import {
	type AppNavigation,
	AutomationExecutionPage,
	AutomationListView,
	OrgAdminPage,
	WorkbenchPage,
	ProjectPage,
	ProjectsHubView,
	Shell,
	SkillMarketView,
	TaskDetailPage,
	NewTaskPage,
} from "@leros/app-ui";

import { Navigate, Route, Routes, useLocation, useNavigate, useParams } from "react-router-dom";

import { DesktopSettingsPage } from "./components/DesktopSettingsPage";

export function AppRoutes() {
	const navigation = useDesktopNavigation();

	return (
		<Shell navigation={navigation}>
			<Routes>
				<Route path="/" element={<Navigate to="/chat" replace />} />

				<Route path="/chat" element={<NewTaskRoutePage />} />

				<Route path="/workbench" element={<WorkbenchRoutePage />} />

				<Route path="/projects" element={<ProjectsHubRoutePage />} />

				<Route path="/projects/:projectId" element={<ProjectRoutePage />} />

				<Route path="/projects/:projectId/tasks" element={<ProjectRoutePage tab="tasks" />} />

				<Route path="/projects/:projectId/files" element={<ProjectRoutePage tab="files" />} />

				<Route path="/projects/:projectId/activity" element={<ProjectRoutePage tab="activity" />} />

				<Route
					path="/projects/:projectId/tasks/:taskId/sessions/:sessionId"
					element={<TaskDetailRoutePage />}
				/>

				<Route path="/org" element={<Navigate to="/org/profile" replace />} />

				<Route path="/org/profile" element={<OrgAdminRoutePage section="profile" />} />

				<Route path="/org/departments" element={<OrgAdminRoutePage section="departments" />} />

				<Route path="/org/assistants" element={<OrgAdminRoutePage section="assistants" />} />

				<Route path="/org/models" element={<OrgAdminRoutePage section="models" />} />

				<Route path="/tasks" element={<EmptyRoutePage />} />

				<Route path="/skills" element={<SkillMarketView navigation={navigation} />} />

				{/* 中文注释：资源库路由暂时隐藏，直接访问时回退到工作台。 */}
				<Route path="/knowledge" element={<Navigate to="/chat" replace />} />

				<Route path="/automation" element={<AutomationListView navigation={navigation} />} />

				<Route path="/automation/:publicId" element={<AutomationExecutionRoutePage />} />

				<Route path="/settings" element={<DesktopSettingsPage />} />

				<Route path="*" element={<Navigate to="/chat" replace />} />
			</Routes>
		</Shell>
	);
}

function useDesktopNavigation(): AppNavigation {
	const location = useLocation();

	const navigate = useNavigate();

	return {
		currentPath: location.pathname,

		goToRoute(route) {
			const routePath = {
				chat: "/chat",

				workbench: "/workbench",

				tasks: "/tasks",

				project: "/chat",

				projectsHub: "/projects",

				taskDetail: "/chat",

				orgProfile: "/org/profile",

				orgDepartments: "/org/departments",

				orgAssistants: "/org/assistants",

				orgModels: "/org/models",

				knowledge: "/knowledge",

				skills: "/skills",

				automation: "/automation",

				settings: "/settings",
			}[route];

			navigate(routePath ?? "/chat");
		},

		goToProject(projectId) {
			navigate(`/projects/${projectId}`);
		},

		goToProjectTasks(projectId) {
			navigate(`/projects/${projectId}/tasks`);
		},

		goToTaskDetail(projectId, taskId, sessionId) {
			navigate(
				`/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}/sessions/${encodeURIComponent(sessionId)}`,
			);
		},

		goToAutomationDetail(publicId) {
			navigate(`/automation/${encodeURIComponent(publicId)}`);
		},
	};
}

function WorkbenchRoutePage() {
	const navigation = useDesktopNavigation();

	return <WorkbenchPage navigation={navigation} />;
}

function NewTaskRoutePage() {
	const navigation = useDesktopNavigation();

	return <NewTaskPage navigation={navigation} />;
}

function projectTabPath(projectId: string, tab: "chat" | "tasks" | "files" | "activity"): string {
	if (tab === "chat") return `/projects/${projectId}`;
	if (tab === "tasks") return `/projects/${projectId}/tasks`;
	if (tab === "files") return `/projects/${projectId}/files`;
	return `/projects/${projectId}/activity`;
}

function ProjectRoutePage({ tab = "chat" }: { tab?: "chat" | "tasks" | "files" | "activity" }) {
	const navigation = useDesktopNavigation();

	const navigate = useNavigate();

	const { projectId = "" } = useParams();

	return (
		<ProjectPage
			projectId={projectId}
			tab={tab}
			navigation={navigation}
			onTabChange={(nextTab) => {
				navigate(projectTabPath(projectId, nextTab));
			}}
		/>
	);
}

function TaskDetailRoutePage() {
	const navigation = useDesktopNavigation();

	const { projectId = "", taskId = "", sessionId = "" } = useParams();

	return (
		<TaskDetailPage
			projectId={projectId}
			taskId={taskId}
			sessionId={sessionId}
			navigation={navigation}
		/>
	);
}

function OrgAdminRoutePage({
	section,
}: {
	section: "profile" | "departments" | "assistants" | "models";
}) {
	const navigation = useDesktopNavigation();

	return <OrgAdminPage section={section} navigation={navigation} />;
}

function EmptyRoutePage() {
	return (
		<div data-slot="empty-page" className="flex min-h-0 flex-1 flex-col bg-[#f7f8fd]">
			<header className="z-10 flex h-20 shrink-0 items-center justify-end px-10" />
		</div>
	);
}

function ProjectsHubRoutePage() {
	const navigation = useDesktopNavigation();

	return <ProjectsHubView navigation={navigation} />;
}

function AutomationExecutionRoutePage() {
	const navigation = useDesktopNavigation();
	const { publicId = "" } = useParams();

	return <AutomationExecutionPage automationPublicId={publicId} navigation={navigation} />;
}
