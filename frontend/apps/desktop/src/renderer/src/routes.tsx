import {
	AiTeammatesView,
	type AppNavigation,
	AssistantListView,
	CenterCanvas,
	ProjectPage,
	ProjectsHubView,
	Shell,
	SkillMarketView,
	TaskDetailPage,
	WorkbenchPanel,
} from "@leros/app-ui";
import {
	Navigate,
	Route,
	Routes,
	useLocation,
	useNavigate,
	useParams,
	useSearchParams,
} from "react-router-dom";
import { DesktopSettingsPage } from "./components/DesktopSettingsPage";

export function AppRoutes() {
	const navigation = useDesktopNavigation();

	return (
		<Shell navigation={navigation}>
			<Routes>
				<Route path="/" element={<Navigate to="/workbench" replace />} />
				<Route path="/workbench" element={<WorkbenchRoutePage />} />
				<Route path="/chat" element={<CenterCanvas />} />
				<Route path="/projects" element={<ProjectsHubRoutePage />} />
				<Route path="/projects/:projectId" element={<ProjectRoutePage />} />
				<Route path="/projects/:projectId/tasks" element={<ProjectRoutePage tab="tasks" />} />
				<Route path="/projects/:projectId/files" element={<ProjectRoutePage tab="files" />} />
				<Route path="/projects/:projectId/tasks/:taskId" element={<TaskDetailRoutePage />} />
				<Route path="/assistants" element={<AssistantListView />} />
				<Route path="/ai-teammates" element={<AiTeammatesView />} />
				<Route path="/tasks" element={<EmptyRoutePage />} />
				<Route path="/skills" element={<SkillMarketView navigation={navigation} />} />
				<Route path="/knowledge" element={<EmptyRoutePage />} />
				<Route path="/settings" element={<DesktopSettingsPage />} />
				<Route path="*" element={<Navigate to="/workbench" replace />} />
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
				project: "/workbench",
				projectsHub: "/projects",
				taskDetail: "/workbench",
				digitalAssistant: "/assistants",
				aiTeammates: "/ai-teammates",
				knowledge: "/knowledge",
				skills: "/skills",
				settings: "/settings",
			}[route];
			navigate(routePath);
		},
		goToProject(projectId) {
			navigate(`/projects/${projectId}`);
		},
		goToTaskDetail(projectId, taskId, sessionId) {
			const search = sessionId ? `?sessionId=${encodeURIComponent(sessionId)}` : "";
			navigate(`/projects/${projectId}/tasks/${taskId}${search}`);
		},
	};
}

function WorkbenchRoutePage() {
	const navigation = useDesktopNavigation();

	return <WorkbenchPanel navigation={navigation} />;
}

function ProjectRoutePage({ tab = "chat" }: { tab?: "chat" | "tasks" | "files" }) {
	const navigation = useDesktopNavigation();
	const navigate = useNavigate();
	const { projectId = "" } = useParams();

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
				navigate(`/projects/${projectId}/${nextTab === "tasks" ? "tasks" : "files"}`);
			}}
		/>
	);
}

function TaskDetailRoutePage() {
	const navigation = useDesktopNavigation();
	const { projectId = "", taskId = "" } = useParams();
	const [searchParams] = useSearchParams();

	return (
		<TaskDetailPage
			projectId={projectId}
			taskId={taskId}
			sessionId={searchParams.get("sessionId")}
			navigation={navigation}
		/>
	);
}

function EmptyRoutePage() {
	return <div data-slot="empty-page" className="min-h-0 flex-1 bg-[#f7f8fd]" />;
}

function ProjectsHubRoutePage() {
	const navigation = useDesktopNavigation();

	return <ProjectsHubView navigation={navigation} />;
}
